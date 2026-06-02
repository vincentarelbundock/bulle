//go:build linux || darwin

package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process-group timeout is Unix-specific")
	}
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
