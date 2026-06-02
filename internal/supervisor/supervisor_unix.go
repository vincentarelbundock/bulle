//go:build linux || darwin

package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

const DefaultGracePeriod = time.Second

var ioctlGetForegroundProcessGroup = func(fd int) (int, error) {
	return unix.IoctlGetInt(fd, unix.TIOCGPGRP)
}

var ioctlSetForegroundProcessGroup = func(fd int, pgid int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgid)
}

var withSIGTTOUBlocked = blockSIGTTOU
var continueProcessGroup = killProcessGroup

type signalNotifier interface {
	Notify(chan<- os.Signal, ...os.Signal)
	Stop(chan<- os.Signal)
}

type processSignalNotifier struct{}

func (processSignalNotifier) Notify(signals chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(signals, sig...)
}

func (processSignalNotifier) Stop(signals chan<- os.Signal) {
	signal.Stop(signals)
}

var supervisorSignalNotifier signalNotifier = processSignalNotifier{}

type Options struct {
	Executable  string
	Timeout     time.Duration
	GracePeriod time.Duration
	Stdin       *os.File
	Stdout      *os.File
	Stderr      *os.File
}

func Run(p policy.Policy, opts Options) error {
	if opts.Timeout <= 0 {
		return fmt.Errorf("supervisor requires a positive timeout")
	}
	executable := opts.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return err
		}
	}
	grace := opts.GracePeriod
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer read.Close()
	defer write.Close()

	forwarder := startSignalForwarder()
	defer forwarder.stop()

	cmd := exec.Command(executable, "__run-prepared-policy", "--policy-fd", "3")
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()
	cmd.ExtraFiles = []*os.File{read}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	term, err := prepareForegroundTerminal(stdin)
	if err != nil {
		return err
	}
	if term != nil {
		defer term.restore()
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	read.Close()

	pgid := cmd.Process.Pid
	if term != nil {
		if err := term.setForeground(pgid); err != nil {
			_ = killProcessGroup(pgid, syscall.SIGKILL)
			_ = write.Close()
			_ = cmd.Wait()
			return finishWithRestore(err, term)
		}
		_ = continueProcessGroup(pgid, syscall.SIGCONT)
	}
	forwarder.setTarget(pgid)

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	var closeWriteOnce sync.Once
	closeWrite := func() error {
		var err error
		closeWriteOnce.Do(func() {
			err = write.Close()
		})
		return err
	}
	defer closeWrite()

	writeDone := make(chan error, 1)
	go func() {
		err := json.NewEncoder(write).Encode(p)
		if closeErr := closeWrite(); err == nil {
			err = closeErr
		}
		writeDone <- err
	}()

	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()

	for {
		select {
		case err := <-waitDone:
			waitDone = nil
			_ = closeWrite()
			if writeDone != nil {
				<-writeDone
			}
			return finishWait(err, term, forwarder.forwardedSignal())
		case err := <-writeDone:
			writeDone = nil
			if err == nil {
				continue
			}
			select {
			case waitErr := <-waitDone:
				waitDone = nil
				return finishWriteError(err, waitErr, term, forwarder.forwardedSignal())
			default:
				_ = killProcessGroup(pgid, syscall.SIGKILL)
				_ = closeWrite()
				waitErr := <-waitDone
				return finishWriteError(err, waitErr, term, forwarder.forwardedSignal())
			}
		case <-timer.C:
			_ = closeWrite()
			return handleTimeout(waitDone, writeDone, term, timeoutContext{
				duration: opts.Timeout,
				grace:    grace,
				pgid:     pgid,
				kill:     killProcessGroup,
			})
		}
	}
}

type timeoutContext struct {
	duration time.Duration
	grace    time.Duration
	pgid     int
	kill     func(int, syscall.Signal) error
}

func handleTimeout(waitDone <-chan error, writeDone <-chan error, term *foregroundTerminal, ctx timeoutContext) error {
	select {
	case waitErr := <-waitDone:
		drainWrite(writeDone)
		return finishWait(waitErr, term, 0)
	default:
	}

	termErr := ctx.kill(ctx.pgid, syscall.SIGTERM)
	if errors.Is(termErr, syscall.ESRCH) {
		waitErr := <-waitDone
		drainWrite(writeDone)
		return finishWait(waitErr, term, 0)
	}

	graceTimer := time.NewTimer(ctx.grace)
	defer graceTimer.Stop()
	waitReaped := false
	select {
	case <-waitDone:
		waitReaped = true
	case <-graceTimer.C:
	}

	_ = ctx.kill(ctx.pgid, syscall.SIGKILL)
	if !waitReaped {
		<-waitDone
	}
	drainWrite(writeDone)
	return finishWithRestore(&TimeoutError{Duration: ctx.duration}, term)
}

func drainWrite(writeDone <-chan error) {
	if writeDone != nil {
		<-writeDone
	}
}

func finishWait(err error, term *foregroundTerminal, forwarded syscall.Signal) error {
	return finishWithRestore(waitError(err, forwarded), term)
}

func finishWriteError(writeErr error, waitErr error, term *foregroundTerminal, forwarded syscall.Signal) error {
	childErr := waitError(waitErr, forwarded)
	if childErr != nil {
		return finishWithRestore(childErr, term)
	}
	return finishWithRestore(writeErr, term)
}

func finishWithRestore(err error, term *foregroundTerminal) error {
	if term == nil {
		return err
	}
	restoreErr := term.restore()
	if restoreErr == nil {
		return err
	}
	restoreErr = &TerminalRestoreError{Err: restoreErr}
	if err == nil {
		return restoreErr
	}
	return errors.Join(err, restoreErr)
}

func waitError(err error, forwarded syscall.Signal) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code < 0 {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
			} else if forwarded != 0 {
				code = 128 + int(forwarded)
			} else {
				code = 1
			}
		}
		return &ExitError{Code: code}
	}
	return err
}

func killProcessGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, sig)
}

type signalForwarder struct {
	signals  chan os.Signal
	done     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	target   int
	pending  []syscall.Signal
	kill     func(int, syscall.Signal) error
	last     atomic.Int64
}

func startSignalForwarder() *signalForwarder {
	forwarder := newSignalForwarder(killProcessGroup)
	signals := make(chan os.Signal, 4)
	supervisorSignalNotifier.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM)
	forwarder.signals = signals
	go func() {
		for {
			select {
			case sig := <-signals:
				if s, ok := sig.(syscall.Signal); ok {
					forwarder.forward(s)
				}
			case <-forwarder.done:
				return
			}
		}
	}()
	return forwarder
}

func newSignalForwarder(kill func(int, syscall.Signal) error) *signalForwarder {
	return &signalForwarder{done: make(chan struct{}), kill: kill}
}

func (f *signalForwarder) stop() {
	f.stopOnce.Do(func() {
		if f.signals != nil {
			supervisorSignalNotifier.Stop(f.signals)
		}
		close(f.done)
	})
}

func (f *signalForwarder) setTarget(pgid int) {
	f.mu.Lock()
	f.target = pgid
	pending := append([]syscall.Signal(nil), f.pending...)
	f.pending = nil
	f.mu.Unlock()

	for _, sig := range pending {
		f.forward(sig)
	}
}

func (f *signalForwarder) forward(sig syscall.Signal) {
	f.last.Store(int64(sig))

	f.mu.Lock()
	pgid := f.target
	if pgid == 0 {
		f.pending = append(f.pending, sig)
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	_ = f.kill(pgid, sig)
}

func (f *signalForwarder) forwardedSignal() syscall.Signal {
	return syscall.Signal(f.last.Load())
}

type foregroundTerminal struct {
	fd          int
	pgid        int
	done        bool
	restoreFunc func() error
}

func prepareForegroundTerminal(file *os.File) (*foregroundTerminal, error) {
	if file == nil {
		return nil, nil
	}
	fd := int(file.Fd())
	pgid, err := ioctlGetForegroundProcessGroup(fd)
	if err != nil {
		if err == unix.ENOTTY || err == unix.ENXIO || err == unix.EINVAL || err == unix.ENODEV || err == unix.ENOTSUP {
			return nil, nil
		}
		return nil, err
	}
	return &foregroundTerminal{fd: fd, pgid: pgid}, nil
}

func (t *foregroundTerminal) restore() error {
	if t == nil || t.done {
		return nil
	}
	if t.restoreFunc != nil {
		err := t.restoreFunc()
		if err == nil {
			t.done = true
		}
		return err
	}
	err := t.setForeground(t.pgid)
	if err != nil {
		return err
	}
	t.done = true
	return nil
}

func (t *foregroundTerminal) setForeground(pgid int) error {
	if t == nil {
		return nil
	}
	return withSIGTTOUBlocked(func() error {
		return ioctlSetForegroundProcessGroup(t.fd, pgid)
	})
}
