//go:build darwin

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireSandboxExec(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	out, err := exec.Command("/usr/bin/sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true").CombinedOutput()
	if err != nil {
		t.Skipf("sandbox-exec unavailable in this environment: %v, output: %s", err, string(out))
	}
}

func TestMacOSSeatbeltDeniesOutsideRead(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, project, "--rw", project, "--env", "PATH", "--", "cat", outside)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("cat outside succeeded, output: %s", string(out))
	}
}

func TestMacOSSeatbeltRunsScriptWithAddExecShebangInterpreter(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	script := filepath.Join(project, "hello")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf shebang-ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, project, "--add-exec", "--add-libs", "--rox", "/bin", "--", "./hello")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v, output: %s", err, string(out))
	}
	if got := strings.TrimSpace(string(out)); got != "shebang-ok" {
		t.Fatalf("script output = %q, want shebang-ok", got)
	}
}

func TestMacOSSeatbeltClosesInheritedFileDescriptors(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	secretPath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret-via-fd"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, err := os.Open(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Close()

	cmd := exec.Command(bin, project, "--rox", "/bin", "--", "/bin/sh", "-c", "cat <&3")
	cmd.ExtraFiles = []*os.File{secret}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("inherited fd read succeeded, output: %s", string(out))
	}
	if strings.Contains(string(out), "secret-via-fd") {
		t.Fatalf("sandboxed command read inherited secret fd: %s", string(out))
	}
}

func TestMacOSSeatbeltRunsFromProjectPath(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	other := filepath.Join(root, "other")
	for _, path := range []string{project, other} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, project, "--rw", project, "--rox", "/bin", "--", "/bin/pwd")
	cmd.Dir = other
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pwd failed: %v, output: %s", err, string(out))
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("pwd = %q, want %q", got, want)
	}
}

func TestMacOSSeatbeltTimeoutExits124(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()

	cmd := exec.Command(bin, "--timeout", "100ms", project, "--rox", "/bin", "--", "/bin/sleep", "5")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("sleep succeeded, output: %s", string(out))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("sleep failed with %T, want *exec.ExitError: %v, output: %s", err, err, string(out))
	}
	if got := exitErr.ExitCode(); got != 124 {
		t.Fatalf("exit code = %d, want 124, output: %s", got, string(out))
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("timeout took %v, want under 2s, output: %s", elapsed, string(out))
	}
	if !strings.Contains(string(out), "command timed out after 100ms") {
		t.Fatalf("output = %q, want timeout message", string(out))
	}
}

func TestMacOSSeatbeltTimeoutKillsBackgroundChild(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	armed := filepath.Join(project, "armed")
	survived := filepath.Join(project, "survived")
	timeoutDuration := "1s"
	childDelaySeconds := "4"
	childDelay := 4 * time.Second
	script := "(printf armed > " + shellQuote(armed) + "; sleep " + childDelaySeconds + "; printf survived > " + shellQuote(survived) + ") & wait"

	cmd := exec.Command(bin, "--timeout", timeoutDuration, project, "--rw", project, "--rox", "/bin", "--", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("background child script succeeded, output: %s", string(out))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("background child script failed with %T, want *exec.ExitError: %v, output: %s", err, err, string(out))
	}
	if got := exitErr.ExitCode(); got != 124 {
		t.Fatalf("exit code = %d, want 124, output: %s", got, string(out))
	}
	if _, err := os.Stat(armed); err != nil {
		t.Fatalf("armed marker missing after timeout: %v, output: %s", err, string(out))
	}
	time.Sleep(childDelay + 200*time.Millisecond)
	if _, err := os.Stat(survived); err == nil {
		t.Fatalf("survived marker exists after process group timeout kill")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat survived marker: %v", err)
	}
}

func TestMacOSSeatbeltTimeoutZeroBehavesLikeNoTimeout(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	truePath := "/bin/true"
	if _, err := os.Stat(truePath); err != nil {
		truePath = "/usr/bin/true"
	}

	cmd := exec.Command(bin, "--timeout", "0", project, "--rox", filepath.Dir(truePath), "--", truePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("true failed: %v, output: %s", err, string(out))
	}
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
