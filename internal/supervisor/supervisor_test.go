//go:build linux || darwin

package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestRunReturnsNilForSuccessfulRunner(t *testing.T) {
	script := helperScript(t, "exit 0\n")
	err := Run(policy.Policy{}, Options{Executable: script, Timeout: time.Second, GracePeriod: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunReturnsExitErrorForFailedRunner(t *testing.T) {
	script := helperScript(t, "exit 7\n")
	err := Run(policy.Policy{}, Options{Executable: script, Timeout: time.Second, GracePeriod: 10 * time.Millisecond})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run error = %T %[1]v, want ExitError", err)
	}
	if exitErr.Code != 7 {
		t.Fatalf("ExitError.Code = %d, want 7", exitErr.Code)
	}
}

func TestRunReturnsTimeoutError(t *testing.T) {
	script := helperScript(t, "sleep 5\n")
	start := time.Now()
	err := Run(policy.Policy{}, Options{Executable: script, Timeout: 50 * time.Millisecond, GracePeriod: 10 * time.Millisecond})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Run error = %T %[1]v, want TimeoutError", err)
	}
	if timeoutErr.Duration != 50*time.Millisecond {
		t.Fatalf("TimeoutError.Duration = %v, want 50ms", timeoutErr.Duration)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v, want under 1s", elapsed)
	}
}

func TestRunTimesOutWhenRunnerDoesNotReadPolicyFD(t *testing.T) {
	script := helperScriptWithoutPolicyRead(t, "sleep 2\n")
	large := strings.Repeat("x", 10*1024*1024)

	start := time.Now()
	err := Run(policy.Policy{Env: map[string]string{"BIG": large}}, Options{Executable: script, Timeout: 50 * time.Millisecond, GracePeriod: 10 * time.Millisecond})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Run error = %T %[1]v, want TimeoutError", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v, want under 1s", elapsed)
	}
}

func TestRunKillsProcessGroupOnTimeout(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	script := helperScript(t, "(sleep 1; printf survived > "+shellQuote(marker)+") & wait\n")

	err := Run(policy.Policy{}, Options{Executable: script, Timeout: 50 * time.Millisecond, GracePeriod: 10 * time.Millisecond})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Run error = %T %[1]v, want TimeoutError", err)
	}

	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("background child survived timeout; marker stat err = %v", err)
	}
}

func TestForegroundTerminalRestoreRetriesAfterFailure(t *testing.T) {
	restoreIoctl := ioctlSetForegroundProcessGroup
	restoreWithSIGTTOUBlocked := withSIGTTOUBlocked
	t.Cleanup(func() {
		ioctlSetForegroundProcessGroup = restoreIoctl
		withSIGTTOUBlocked = restoreWithSIGTTOUBlocked
	})

	restoreErr := errors.New("restore failed")
	attempts := 0
	ioctlSetForegroundProcessGroup = func(fd int, value int) error {
		attempts++
		if fd != 9 || value != 42 {
			t.Fatalf("restore ioctl args = (%d, %d), want (9, 42)", fd, value)
		}
		if attempts == 1 {
			return restoreErr
		}
		return nil
	}

	blockedCalls := 0
	withSIGTTOUBlocked = func(fn func() error) error {
		blockedCalls++
		return fn()
	}

	term := &foregroundTerminal{fd: 9, pgid: 42}
	if err := term.restore(); !errors.Is(err, restoreErr) {
		t.Fatalf("first restore error = %v, want %v", err, restoreErr)
	}
	if term.done {
		t.Fatal("restore marked done after failed ioctl")
	}
	if err := term.restore(); err != nil {
		t.Fatalf("second restore error = %v, want nil", err)
	}
	if !term.done {
		t.Fatal("restore did not mark done after successful ioctl")
	}
	if attempts != 2 {
		t.Fatalf("restore attempts = %d, want 2", attempts)
	}
	if blockedCalls != 2 {
		t.Fatalf("withSIGTTOUBlocked calls = %d, want 2", blockedCalls)
	}
}

func TestRunReturnsShellStatusForForwardedSignal(t *testing.T) {
	restoreNotifier := supervisorSignalNotifier
	fake := newFakeSignalNotifier()
	supervisorSignalNotifier = fake
	t.Cleanup(func() {
		supervisorSignalNotifier = restoreNotifier
	})

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	script := helperScript(t, "printf ready > "+shellQuote(ready)+"\nsleep 5\n")
	errc := make(chan error, 1)
	go func() {
		errc <- Run(policy.Policy{}, Options{Executable: script, Timeout: 5 * time.Second, GracePeriod: 10 * time.Millisecond})
	}()

	signalc := fake.waitForNotify(t)
	waitForFile(t, ready)
	signalc <- syscall.SIGTERM

	var err error
	select {
	case err = <-errc:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after forwarded SIGTERM")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run error = %T %[1]v, want ExitError", err)
	}
	if exitErr.Code != 128+int(syscall.SIGTERM) {
		t.Fatalf("ExitError.Code = %d, want %d", exitErr.Code, 128+int(syscall.SIGTERM))
	}
	fake.waitForStop(t)
}

func helperScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" != \"__run-prepared-policy\" ] || [ \"$2\" != \"--policy-fd\" ]; then\n" +
		"  echo bad runner args >&2\n" +
		"  exit 64\n" +
		"fi\n" +
		"cat <&3 >/dev/null\n" +
		body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func helperScriptWithoutPolicyRead(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" != \"__run-prepared-policy\" ] || [ \"$2\" != \"--policy-fd\" ]; then\n" +
		"  echo bad runner args >&2\n" +
		"  exit 64\n" +
		"fi\n" +
		body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(value string) string {
	out := "'"
	for _, r := range value {
		if r == '\'' {
			out += "'\\''"
			continue
		}
		out += string(r)
	}
	return out + "'"
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

type fakeSignalNotifier struct {
	ready   chan chan<- os.Signal
	stopped chan struct{}
}

func newFakeSignalNotifier() *fakeSignalNotifier {
	return &fakeSignalNotifier{
		ready:   make(chan chan<- os.Signal, 1),
		stopped: make(chan struct{}, 1),
	}
}

func (f *fakeSignalNotifier) Notify(signals chan<- os.Signal, _ ...os.Signal) {
	f.ready <- signals
}

func (f *fakeSignalNotifier) Stop(chan<- os.Signal) {
	f.stopped <- struct{}{}
}

func (f *fakeSignalNotifier) waitForNotify(t *testing.T) chan<- os.Signal {
	t.Helper()
	select {
	case signalc := <-f.ready:
		return signalc
	case <-time.After(time.Second):
		t.Fatal("signal notifier was not registered")
		return nil
	}
}

func (f *fakeSignalNotifier) waitForStop(t *testing.T) {
	t.Helper()
	select {
	case <-f.stopped:
	case <-time.After(time.Second):
		t.Fatal("signal notifier was not stopped")
	}
}
