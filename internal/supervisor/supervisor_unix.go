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

var withSIGTTOUBlocked = func(fn func() error) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTTOU)
	defer signal.Stop(signals)
	return fn()
}

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
		cmd.SysProcAttr.Foreground = true
		cmd.SysProcAttr.Ctty = int(stdin.Fd())
		defer term.restore()
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	read.Close()

	pgid := cmd.Process.Pid
	forwarder := forwardSignals(pgid)
	defer forwarder.stop()

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
				return finishWait(waitErr, term, forwarder.forwardedSignal())
			default:
				_ = killProcessGroup(pgid, syscall.SIGKILL)
				_ = closeWrite()
				<-waitDone
				return finishWithRestore(err, term)
			}
		case <-timer.C:
			_ = killProcessGroup(pgid, syscall.SIGTERM)
			graceTimer := time.NewTimer(grace)
			var waitErr error
			select {
			case waitErr = <-waitDone:
				waitDone = nil
			case <-graceTimer.C:
				_ = killProcessGroup(pgid, syscall.SIGKILL)
				waitErr = <-waitDone
				waitDone = nil
			}
			graceTimer.Stop()
			_ = waitErr
			_ = closeWrite()
			if writeDone != nil {
				<-writeDone
			}
			if term != nil {
				_ = term.restore()
			}
			return &TimeoutError{Duration: opts.Timeout}
		}
	}
}

func finishWait(err error, term *foregroundTerminal, forwarded syscall.Signal) error {
	return finishWithRestore(waitError(err, forwarded), term)
}

func finishWithRestore(err error, term *foregroundTerminal) error {
	if term == nil {
		return err
	}
	restoreErr := term.restore()
	if restoreErr == nil {
		return err
	}
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
			if forwarded != 0 {
				code = 128 + int(forwarded)
			} else if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
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
	err := syscall.Kill(-pgid, sig)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

type signalForwarder struct {
	signals chan os.Signal
	done    chan struct{}
	last    atomic.Int64
}

func forwardSignals(pgid int) *signalForwarder {
	signals := make(chan os.Signal, 4)
	supervisorSignalNotifier.Notify(signals, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM)
	forwarder := &signalForwarder{signals: signals, done: make(chan struct{})}
	go func() {
		for {
			select {
			case sig := <-signals:
				if s, ok := sig.(syscall.Signal); ok {
					forwarder.last.Store(int64(s))
					_ = killProcessGroup(pgid, s)
				}
			case <-forwarder.done:
				return
			}
		}
	}()
	return forwarder
}

func (f *signalForwarder) stop() {
	supervisorSignalNotifier.Stop(f.signals)
	close(f.done)
}

func (f *signalForwarder) forwardedSignal() syscall.Signal {
	return syscall.Signal(f.last.Load())
}

type foregroundTerminal struct {
	fd   int
	pgid int
	done bool
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
	err := withSIGTTOUBlocked(func() error {
		return ioctlSetForegroundProcessGroup(t.fd, t.pgid)
	})
	if err != nil {
		return err
	}
	t.done = true
	return nil
}
