//go:build linux || darwin

package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

const DefaultGracePeriod = time.Second

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
	stopForwarding := forwardSignals(pgid)
	defer stopForwarding()

	if err := json.NewEncoder(write).Encode(p); err != nil {
		killProcessGroup(pgid, syscall.SIGKILL)
		_ = cmd.Wait()
		return err
	}
	write.Close()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if term != nil {
			if restoreErr := term.restore(); restoreErr != nil && err == nil {
				return restoreErr
			}
		}
		return waitError(err)
	case <-timer.C:
		killProcessGroup(pgid, syscall.SIGTERM)
		graceTimer := time.NewTimer(grace)
		defer graceTimer.Stop()
		select {
		case <-done:
		case <-graceTimer.C:
			killProcessGroup(pgid, syscall.SIGKILL)
			<-done
		}
		if term != nil {
			_ = term.restore()
		}
		return &TimeoutError{Duration: opts.Timeout}
	}
}

func waitError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code < 0 {
			code = 1
		}
		return &ExitError{Code: code}
	}
	return err
}

func killProcessGroup(pgid int, sig syscall.Signal) {
	if pgid <= 0 {
		return
	}
	err := syscall.Kill(-pgid, sig)
	if err == syscall.ESRCH {
		return
	}
}

func forwardSignals(pgid int) func() {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-signals:
				if s, ok := sig.(syscall.Signal); ok {
					killProcessGroup(pgid, s)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
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
	pgid, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
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
	t.done = true
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	return unix.IoctlSetPointerInt(t.fd, unix.TIOCSPGRP, t.pgid)
}
