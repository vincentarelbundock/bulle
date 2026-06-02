# Timeout Flag Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe cross-platform `--timeout DURATION` flag that terminates sandboxed commands after a Go-style wall-clock duration.

**Architecture:** Parse timeout once in `internal/cli`, carry it through `policy.Policy` and policy views, and enforce it with a new `internal/supervisor` package. The supervisor starts a hidden prepared-policy runner in a separate process group, passes the prepared policy over an inherited fd, kills the process group on timeout, and maps timeout to exit code `124`.

**Tech Stack:** Go 1.22, Kong CLI parsing, `os/exec`, `syscall`, `golang.org/x/sys/unix`, existing Linux Landlock and macOS Seatbelt backends, standard `testing`.

---

## File Structure

- Modify `internal/cli/options.go`: add typed `Timeout time.Duration` to parsed options.
- Modify `internal/cli/parse.go`: add raw `--timeout` CLI flag and parse it with `time.ParseDuration`.
- Modify `internal/cli/parse_test.go`: add parser and help coverage for timeout.
- Modify `internal/cli/usage.go`: document `--timeout`.
- Modify `internal/policy/policy.go`: add timeout to `Policy` and policy JSON view.
- Modify `internal/policy/view.go`: format timeout for public policy views.
- Modify `internal/policy/view_test.go`: verify view timeout formatting and omission.
- Modify `internal/policy/resolve.go`: copy timeout from CLI options into resolved policy.
- Modify `internal/policy/resolve_test.go`: verify policy resolution carries timeout.
- Modify `internal/app/profile_summary.go`: show timeout in human-readable policy summary when set.
- Modify `internal/app/profile_summary_test.go`: verify summary timeout line.
- Modify `internal/app/app.go`: add `ExitTimedOut`, dispatch hidden runner mode before public CLI parsing, and call supervisor when `p.Timeout > 0`.
- Create `internal/app/runner.go`: implement hidden prepared-policy runner.
- Modify `internal/app/app_test.go`: cover hidden runner argument errors and timeout policy output.
- Create `internal/supervisor/errors.go`: define `TimeoutError` and `ExitError`.
- Create `internal/supervisor/supervisor_unix.go`: implement Linux/macOS supervised execution.
- Create `internal/supervisor/supervisor_stub.go`: return a setup error for unsupported platforms.
- Create `internal/supervisor/supervisor_test.go`: test success, failure, timeout, and process-group cleanup with shell helper scripts.
- Modify `internal/integration/linux_landlock_test.go`: add Linux timeout integration tests.
- Modify `internal/integration/macos_seatbelt_test.go`: add macOS timeout integration tests.
- Modify `docs-src/cli-reference.md`: regenerate from `go run ./cmd/bulle-docs`.
- Modify generated `docs/`: regenerate with `make website`.

## Task 1: CLI Timeout Parsing and Help Text

**Files:**
- Modify: `internal/cli/options.go`
- Modify: `internal/cli/parse.go`
- Modify: `internal/cli/parse_test.go`
- Modify: `internal/cli/usage.go`

- [ ] **Step 1: Write failing parser tests**

Add `time` to the import list in `internal/cli/parse_test.go`:

```go
import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/config"
)
```

Add these tests after `TestParseInstallProfilesFlag`:

```go
func TestParseTimeoutFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: "space separated seconds", args: []string{"bulle", "--timeout", "30s", "--", "echo"}, want: 30 * time.Second},
		{name: "equals combined duration", args: []string{"bulle", "--timeout=1h30m", "--", "echo"}, want: 90 * time.Minute},
		{name: "omitted timeout", args: []string{"bulle", "--", "echo"}, want: 0},
		{name: "plain zero disables", args: []string{"bulle", "--timeout", "0", "--", "echo"}, want: 0},
		{name: "zero with unit disables", args: []string{"bulle", "--timeout", "0s", "--", "echo"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if opts.Timeout != tt.want {
				t.Fatalf("Timeout = %v, want %v", opts.Timeout, tt.want)
			}
		})
	}
}

func TestParseTimeoutRejectsInvalidValues(t *testing.T) {
	tests := []string{"30", "-1s", "ten seconds"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			_, err := Parse([]string{"bulle", "--timeout", value, "--", "echo"})
			if err == nil {
				t.Fatal("Parse returned nil error, want timeout validation error")
			}
			if !strings.Contains(err.Error(), `invalid --timeout value "`+value+`"`) {
				t.Fatalf("Parse error = %q, want invalid timeout value", err.Error())
			}
			if !strings.Contains(err.Error(), "30s") || !strings.Contains(err.Error(), "1h30m") {
				t.Fatalf("Parse error = %q, want Go duration examples", err.Error())
			}
		})
	}
}
```

Extend `TestUsageShowsProfileShortFlag` with:

```go
	if !strings.Contains(Usage(), "--timeout DURATION") {
		t.Fatalf("Usage() does not show --timeout:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "Go duration") {
		t.Fatalf("Usage() does not explain Go duration syntax:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "exit 124") {
		t.Fatalf("Usage() does not document timeout exit code:\n%s", Usage())
	}
```

- [ ] **Step 2: Run parser tests and verify failure**

Run:

```bash
go test ./internal/cli
```

Expected: FAIL because `Options.Timeout` and `--timeout` do not exist.

- [ ] **Step 3: Add typed timeout to CLI options**

Change `internal/cli/options.go` to:

```go
package cli

import "time"

type Options struct {
	Flags

	ProjectPath  string
	Command      []string
	PolicyFormat string
	Timeout      time.Duration
}
```

- [ ] **Step 4: Add raw flag and parser validation**

In `internal/cli/parse.go`, add `time` to imports:

```go
import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alecthomas/kong"
)
```

Add this field to `Flags` near the other safety/output flags:

```go
	Timeout string `name:"timeout" placeholder:"DURATION" help:"Kill the sandboxed command if it runs longer than DURATION, using Go duration syntax such as 30s, 2m, or 1h30m; 0 disables."`
```

After `opts.Flags = parsed.Flags` in `Parse`, add:

```go
	timeout, err := parseTimeout(parsed.Timeout)
	if err != nil {
		return opts, err
	}
	opts.Timeout = timeout
```

Add this helper below `parseKong`:

```go
func parseTimeout(value string) (time.Duration, error) {
	if value == "" || value == "0" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("invalid --timeout value %q; use a Go duration such as 30s, 2m, or 1h30m", value)
	}
	return duration, nil
}
```

- [ ] **Step 5: Update help text**

In `internal/cli/usage.go`, update the "Output and safety" section to include timeout before `--policy`:

```text
Output and safety:
  --timeout DURATION
                    kill the sandboxed command if it runs longer than DURATION.
                    Uses Go duration syntax such as 30s, 2m, or 1h30m.
                    Use 0 to disable. Timed-out commands exit 124.
  --policy[=summary|json]
                    print the resolved policy and exit; summary by default
```

- [ ] **Step 6: Run parser tests and verify pass**

Run:

```bash
go test ./internal/cli
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/options.go internal/cli/parse.go internal/cli/parse_test.go internal/cli/usage.go
git commit -m "feat: parse timeout flag"
```

## Task 2: Policy Timeout Propagation and Policy Output

**Files:**
- Modify: `internal/policy/policy.go`
- Modify: `internal/policy/view.go`
- Modify: `internal/policy/view_test.go`
- Modify: `internal/policy/resolve.go`
- Modify: `internal/policy/resolve_test.go`
- Modify: `internal/app/profile_summary.go`
- Modify: `internal/app/profile_summary_test.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Write failing policy/view tests**

Add `time` to `internal/policy/view_test.go` imports and extend the file:

```go
package policy

import (
	"testing"
	"time"
)
```

Add:

```go
func TestNewViewFormatsTimeout(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{}, Timeout: 90 * time.Second})
	if view.Timeout != "1m30s" {
		t.Fatalf("Timeout = %q, want 1m30s", view.Timeout)
	}
}

func TestNewViewOmitsZeroTimeout(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{}})
	if view.Timeout != "" {
		t.Fatalf("Timeout = %q, want empty", view.Timeout)
	}
}
```

Add `time` to `internal/policy/resolve_test.go` imports and add:

```go
func TestResolveCarriesTimeoutFromOptions(t *testing.T) {
	project := t.TempDir()
	cfg := config.Config{Profiles: map[string]config.Profile{"default": {}}}
	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Timeout: 45 * time.Second},
		Global:    cfg,
		ParentEnv: map[string]string{},
		Home:      t.TempDir(),
		Tmp:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.Timeout != 45*time.Second {
		t.Fatalf("Timeout = %v, want 45s", got.Timeout)
	}
}
```

Add `time` to `internal/app/profile_summary_test.go` imports and add:

```go
func TestWriteProfilePermissionSummaryIncludesTimeoutWhenSet(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendLinuxLandlock,
		ProjectPath: "/tmp/project",
		Command:     []string{"sleep", "60"},
		Env:         map[string]string{},
		Timeout:     30 * time.Second,
	}

	writeProfilePermissionSummary("agent", p, &stderr)

	assertContains(t, stderr.String(), "  timeout: 30s\n")
}
```

Add this test after `TestRunPolicyJSONPrintsResolvedPolicy` in `internal/app/app_test.go`:

```go
func TestRunPolicyJSONIncludesTimeoutWhenConfigured(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--timeout", "30s", "--profile", "tool", tmp, "--policy=json", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"timeout":"30s"`)) {
		t.Fatalf("stdout missing timeout: %s", stdout.String())
	}
}

func TestRunPolicyJSONOmitsTimeoutWhenUnset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--profile", "tool", tmp, "--policy=json", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte(`"timeout"`)) {
		t.Fatalf("stdout unexpectedly contains timeout: %s", stdout.String())
	}
}
```

- [ ] **Step 2: Run policy and app tests and verify failure**

Run:

```bash
go test ./internal/policy ./internal/app
```

Expected: FAIL because timeout fields are not present.

- [ ] **Step 3: Add timeout to policy model**

Change `internal/policy/policy.go` to import `time`:

```go
package policy

import "time"
```

Add this field to `Policy` after `Command`:

```go
	Timeout time.Duration
```

Add this field to `View` after `Command`:

```go
	Timeout string `json:"timeout,omitempty"`
```

- [ ] **Step 4: Format timeout in policy view**

In `internal/policy/view.go`, compute timeout before the `return View{...}`:

```go
	timeout := ""
	if p.Timeout > 0 {
		timeout = p.Timeout.String()
	}
```

Add `Timeout: timeout,` to the returned `View`.

- [ ] **Step 5: Resolve timeout from CLI options**

In `internal/policy/resolve.go`, set the field after `p.Command = in.Options.Command`:

```go
	p.Timeout = in.Options.Timeout
```

- [ ] **Step 6: Show timeout in human-readable summaries**

In `internal/app/profile_summary.go`, add this after the `network` line:

```go
	if view.Timeout != "" {
		fmt.Fprintf(w, "  timeout: %s\n", view.Timeout)
	}
```

- [ ] **Step 7: Run policy and app tests and verify pass**

Run:

```bash
go test ./internal/policy ./internal/app
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/policy/policy.go internal/policy/view.go internal/policy/view_test.go internal/policy/resolve.go internal/policy/resolve_test.go internal/app/profile_summary.go internal/app/profile_summary_test.go internal/app/app_test.go
git commit -m "feat: include timeout in resolved policy"
```

## Task 3: Hidden Prepared-Policy Runner

**Files:**
- Modify: `internal/app/app.go`
- Create: `internal/app/runner.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Write failing runner argument tests**

Add these tests to `internal/app/app_test.go`:

```go
func TestRunPreparedPolicyRejectsMissingFD(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "__run-prepared-policy"}, &stdout, &stderr)

	if code != ExitSandboxSetup {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitSandboxSetup, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("usage: bulle __run-prepared-policy --policy-fd FD")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunPreparedPolicyRejectsInvalidFD(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "__run-prepared-policy", "--policy-fd", "not-a-number"}, &stdout, &stderr)

	if code != ExitSandboxSetup {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitSandboxSetup, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`invalid policy fd "not-a-number"`)) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
```

- [ ] **Step 2: Run app tests and verify failure**

Run:

```bash
go test ./internal/app
```

Expected: FAIL because `__run-prepared-policy` is parsed as public CLI input.

- [ ] **Step 3: Dispatch hidden runner before public CLI parsing**

At the top of `Run` in `internal/app/app.go`, before `cli.Parse(args)`, add:

```go
	if isPreparedPolicyRunner(args) {
		return runPreparedPolicy(args, stdout, stderr)
	}
```

- [ ] **Step 4: Create runner implementation**

Create `internal/app/runner.go`:

```go
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/vincentarelbundock/bulle/internal/backends"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

const preparedPolicyRunnerCommand = "__run-prepared-policy"

func isPreparedPolicyRunner(args []string) bool {
	return len(args) > 1 && args[1] == preparedPolicyRunnerCommand
}

func runPreparedPolicy(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 4 || args[2] != "--policy-fd" {
		fmt.Fprintln(stderr, "usage: bulle __run-prepared-policy --policy-fd FD")
		return ExitSandboxSetup
	}
	fd, err := strconv.Atoi(args[3])
	if err != nil || fd < 0 {
		fmt.Fprintf(stderr, "invalid policy fd %q\n", args[3])
		return ExitSandboxSetup
	}
	file := os.NewFile(uintptr(fd), "prepared-policy")
	if file == nil {
		fmt.Fprintf(stderr, "invalid policy fd %q\n", args[3])
		return ExitSandboxSetup
	}
	defer file.Close()

	var p policy.Policy
	if err := json.NewDecoder(file).Decode(&p); err != nil {
		fmt.Fprintf(stderr, "decode prepared policy: %v\n", err)
		return ExitSandboxSetup
	}
	return runPreparedPolicyBackend(p, stderr)
}

func runPreparedPolicyBackend(p policy.Policy, stderr io.Writer) int {
	backend, err := backends.ForName(p.Backend)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitBackendMissing
	}
	if err := backend.Run(p); err != nil {
		fmt.Fprintln(stderr, err)
		if isCommandExitError(err) {
			return ExitCommandFailed
		}
		return ExitSandboxSetup
	}
	return ExitOK
}
```

- [ ] **Step 5: Run app tests and verify pass**

Run:

```bash
go test ./internal/app
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/runner.go internal/app/app_test.go
git commit -m "feat: add prepared policy runner"
```

## Task 4: Supervisor Package

**Files:**
- Create: `internal/supervisor/errors.go`
- Create: `internal/supervisor/supervisor_unix.go`
- Create: `internal/supervisor/supervisor_stub.go`
- Create: `internal/supervisor/supervisor_test.go`

- [ ] **Step 1: Write failing supervisor tests**

Create `internal/supervisor/supervisor_test.go`:

```go
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
```

- [ ] **Step 2: Run supervisor tests and verify failure**

Run:

```bash
go test ./internal/supervisor
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Add supervisor error types**

Create `internal/supervisor/errors.go`:

```go
package supervisor

import (
	"fmt"
	"time"
)

type TimeoutError struct {
	Duration time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("command timed out after %s", e.Duration)
}

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("runner exited with status %d", e.Code)
}
```

- [ ] **Step 4: Add Unix supervisor implementation**

Create `internal/supervisor/supervisor_unix.go`:

```go
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
		if err == unix.ENOTTY || err == unix.ENXIO || err == unix.EINVAL {
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
```

- [ ] **Step 5: Add unsupported-platform stub**

Create `internal/supervisor/supervisor_stub.go`:

```go
//go:build !linux && !darwin

package supervisor

import (
	"fmt"
	"os"
	"time"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

type Options struct {
	Executable  string
	Timeout     time.Duration
	GracePeriod time.Duration
	Stdin       *os.File
	Stdout      *os.File
	Stderr      *os.File
}

func Run(policy.Policy, Options) error {
	return fmt.Errorf("timeout supervision is only supported on linux and darwin")
}
```

- [ ] **Step 6: Run supervisor tests and verify pass**

Run:

```bash
go test ./internal/supervisor
```

Expected: PASS on Linux and macOS.

- [ ] **Step 7: Commit**

```bash
git add internal/supervisor/errors.go internal/supervisor/supervisor_unix.go internal/supervisor/supervisor_stub.go internal/supervisor/supervisor_test.go
git commit -m "feat: add timeout supervisor"
```

## Task 5: Wire Timeout Supervisor Into App Run

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Write failing app timeout exit-code tests**

Add these tests to `internal/app/app_test.go`:

```go
func TestRunDefinesTimeoutExitCode(t *testing.T) {
	if ExitTimedOut != 124 {
		t.Fatalf("ExitTimedOut = %d, want 124", ExitTimedOut)
	}
}

func TestRunPolicySummaryIncludesTimeoutWhenConfigured(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--timeout", "30s", "--profile", "tool", tmp, "--policy", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("  timeout: 30s\n")) {
		t.Fatalf("stdout missing timeout summary: %s", stdout.String())
	}
}
```

- [ ] **Step 2: Run app tests and verify failure**

Run:

```bash
go test ./internal/app
```

Expected: FAIL because `ExitTimedOut` and supervisor wiring do not exist.

- [ ] **Step 3: Add app timeout exit mapping**

In `internal/app/app.go`, add `ExitTimedOut` to the exit-code constants:

```go
	ExitTimedOut         = 124
```

Add imports:

```go
	"github.com/vincentarelbundock/bulle/internal/supervisor"
```

After `p.Command = commandWithSessionPermissions(...)`, replace direct backend execution with:

```go
	if p.Timeout > 0 {
		if err := supervisor.Run(p, supervisor.Options{
			Timeout: p.Timeout,
			Stdin:   os.Stdin,
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
		}); err != nil {
			return exitCodeForSupervisorError(err, stderr)
		}
		return ExitOK
	}
	if err := backend.Run(p); err != nil {
		fmt.Fprintln(stderr, err)
		if isCommandExitError(err) {
			return ExitCommandFailed
		}
		return ExitSandboxSetup
	}
	return ExitOK
```

Add this helper near `isCommandExitError`:

```go
func exitCodeForSupervisorError(err error, stderr io.Writer) int {
	var timeoutErr *supervisor.TimeoutError
	if errors.As(err, &timeoutErr) {
		fmt.Fprintf(stderr, "bulle: command timed out after %s\n", timeoutErr.Duration)
		return ExitTimedOut
	}
	var exitErr *supervisor.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Code > 0 {
			return exitErr.Code
		}
		return ExitCommandFailed
	}
	fmt.Fprintln(stderr, err)
	return ExitSandboxSetup
}
```

- [ ] **Step 4: Run app tests and verify pass**

Run:

```bash
go test ./internal/app
```

Expected: PASS.

- [ ] **Step 5: Run all unit tests touched so far**

Run:

```bash
go test ./internal/cli ./internal/policy ./internal/app ./internal/supervisor
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "feat: enforce timeout from app"
```

## Task 6: Linux Timeout Integration Tests

**Files:**
- Modify: `internal/integration/linux_landlock_test.go`

- [ ] **Step 1: Add failing Linux integration tests**

Add imports to `internal/integration/linux_landlock_test.go`:

```go
	"time"
```

Add these tests after `TestLinuxLandlockRunsFromProjectPath`:

```go
func TestLinuxLandlockTimeoutExits124(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	args := append([]string{"--timeout", "100ms", project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "/bin/sleep", "5")

	start := time.Now()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("sleep succeeded, want timeout; output: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 124 {
		t.Fatalf("exit = %v, want status 124; output: %s", err, string(out))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %v, want under 2s; output: %s", elapsed, string(out))
	}
	if !strings.Contains(string(out), "command timed out after 100ms") {
		t.Fatalf("output missing timeout message: %s", string(out))
	}
}

func TestLinuxLandlockTimeoutKillsBackgroundChild(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	marker := filepath.Join(project, "survived")
	script := "(sleep 1; printf survived > " + shellQuote(marker) + ") & wait"
	args := append([]string{"--timeout", "100ms", project, "--rw", project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "/bin/sh", "-c", script)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("shell succeeded, want timeout; output: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 124 {
		t.Fatalf("exit = %v, want status 124; output: %s", err, string(out))
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("background child survived timeout; marker stat err = %v; output: %s", err, string(out))
	}
}

func TestLinuxLandlockTimeoutZeroBehavesLikeNoTimeout(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	args := append([]string{"--timeout", "0", project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "/bin/true")

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("true failed: %v, output: %s", err, string(out))
	}
}
```

Add this helper at the bottom of the file:

```go
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
```

- [ ] **Step 2: Run Linux integration test and verify behavior**

Run on Linux:

```bash
go test ./internal/integration -run 'TestLinuxLandlockTimeout' -count=1 -v
```

Expected: PASS after Tasks 1-5 are complete. If the background-child test fails, inspect whether the supervisor is killing only the runner pid instead of the process group.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/linux_landlock_test.go
git commit -m "test: cover linux timeout behavior"
```

## Task 7: macOS Timeout Integration Tests

**Files:**
- Modify: `internal/integration/macos_seatbelt_test.go`

- [ ] **Step 1: Add failing macOS integration tests**

Add `time` to `internal/integration/macos_seatbelt_test.go` imports.

Add these tests after `TestMacOSSeatbeltRunsFromProjectPath`:

```go
func TestMacOSSeatbeltTimeoutExits124(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()

	start := time.Now()
	cmd := exec.Command(bin, "--timeout", "100ms", project, "--rox", "/bin", "--", "/bin/sleep", "5")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("sleep succeeded, want timeout; output: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 124 {
		t.Fatalf("exit = %v, want status 124; output: %s", err, string(out))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %v, want under 2s; output: %s", elapsed, string(out))
	}
	if !strings.Contains(string(out), "command timed out after 100ms") {
		t.Fatalf("output missing timeout message: %s", string(out))
	}
}

func TestMacOSSeatbeltTimeoutKillsBackgroundChild(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	marker := filepath.Join(project, "survived")
	script := "(sleep 1; printf survived > " + shellQuote(marker) + ") & wait"

	cmd := exec.Command(bin, "--timeout", "100ms", project, "--rw", project, "--rox", "/bin", "--", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("shell succeeded, want timeout; output: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 124 {
		t.Fatalf("exit = %v, want status 124; output: %s", err, string(out))
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("background child survived timeout; marker stat err = %v; output: %s", err, string(out))
	}
}

func TestMacOSSeatbeltTimeoutZeroBehavesLikeNoTimeout(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()

	cmd := exec.Command(bin, "--timeout", "0", project, "--rox", "/bin", "--", "/bin/true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("true failed: %v, output: %s", err, string(out))
	}
}
```

Add this helper at the bottom of the file:

```go
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
```

- [ ] **Step 2: Run macOS integration tests and verify behavior**

Run on macOS:

```bash
go test ./internal/integration -run 'TestMacOSSeatbeltTimeout|TestMacOSSeatbeltAllowsInheritedTerminalIoctl' -count=1 -v
```

Expected: PASS. The terminal ioctl regression test must continue to pass because timeout supervision must not break inherited terminal behavior.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/macos_seatbelt_test.go
git commit -m "test: cover macos timeout behavior"
```

## Task 8: Documentation Generation

**Files:**
- Modify: `docs-src/cli-reference.md`
- Modify: generated files under `docs/`

- [ ] **Step 1: Regenerate CLI reference source**

Run:

```bash
go run ./cmd/bulle-docs
```

Expected: `docs-src/cli-reference.md` includes `--timeout DURATION`, Go duration examples, and exit `124`.

- [ ] **Step 2: Build generated website**

Run:

```bash
make website
```

Expected: command exits 0 and generated `docs/cli-reference.html` includes the timeout flag.

- [ ] **Step 3: Inspect docs diff**

Run:

```bash
git diff -- docs-src/cli-reference.md docs/cli-reference.html docs/search.json
```

Expected: diff only reflects the new timeout CLI reference text and regenerated search data.

- [ ] **Step 4: Commit**

```bash
git add docs-src/cli-reference.md docs/
git commit -m "docs: document timeout flag"
```

## Task 9: Final Verification

**Files:**
- All modified Go files
- Generated documentation files

- [ ] **Step 1: Format Go files**

Run:

```bash
gofmt -w internal/cli/options.go internal/cli/parse.go internal/cli/parse_test.go internal/policy/policy.go internal/policy/view.go internal/policy/view_test.go internal/policy/resolve.go internal/policy/resolve_test.go internal/app/app.go internal/app/runner.go internal/app/app_test.go internal/app/profile_summary.go internal/app/profile_summary_test.go internal/supervisor/errors.go internal/supervisor/supervisor_unix.go internal/supervisor/supervisor_stub.go internal/supervisor/supervisor_test.go internal/integration/linux_landlock_test.go internal/integration/macos_seatbelt_test.go
```

Expected: command exits 0.

- [ ] **Step 2: Run full Go test suite**

Run:

```bash
go test ./...
```

Expected: PASS on the current platform.

- [ ] **Step 3: Run repository checks**

Run:

```bash
make check
```

Expected: PASS.

- [ ] **Step 4: Verify timeout manually with built binary**

Run:

```bash
go build -o /tmp/bulle-timeout ./cmd/bulle
/tmp/bulle-timeout --timeout 100ms --add-exec -- /bin/sleep 5
```

Expected: exits with status `124` and prints:

```text
bulle: command timed out after 100ms
```

- [ ] **Step 5: Inspect final diff**

Run:

```bash
git status --short
git diff --stat HEAD
```

Expected: worktree contains only the timeout feature changes after the last committed task, or is clean if every task has been committed.

- [ ] **Step 6: Commit any formatting-only changes**

If Step 1 changed files after the task commits, run:

```bash
git add internal docs-src docs
git commit -m "chore: format timeout implementation"
```

Expected: commit succeeds only if formatting changed files.
