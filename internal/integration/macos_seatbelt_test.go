//go:build darwin

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	cmd := exec.Command(bin, "default", project, "--rw", project, "--env", "PATH", "--", "cat", outside)
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
	// No profile is named, so the workspace comes from the working directory
	// and library discovery is on — which is what grants /var/select, the
	// selector /bin/sh reads to decide which shell it is.
	cmd := exec.Command(bin, "--rox", "/bin", "--", "./hello")
	cmd.Dir = project
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

	cmd := exec.Command(bin, "default", project, "--rox", "/bin", "--", "/bin/sh", "-c", "cat <&3")
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
	cmd := exec.Command(bin, "default", project, "--rw", project, "--rox", "/bin", "--", "/bin/pwd")
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

func TestMacOSSeatbeltRejectsTimeoutWithoutSecureTreeContainer(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()

	cmd := exec.Command(bin, "--timeout", "100ms", "default", project, "--rox", "/bin", "--", "/bin/sleep", "5")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("timeout run succeeded without secure tree containment, output: %s", string(out))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("sleep failed with %T, want *exec.ExitError: %v, output: %s", err, err, string(out))
	}
	if got := exitErr.ExitCode(); got != 5 {
		t.Fatalf("exit code = %d, want sandbox setup failure 5, output: %s", got, string(out))
	}
	if !strings.Contains(string(out), "cannot securely contain descendants that call setsid") {
		t.Fatalf("output = %q, want fail-closed timeout explanation", string(out))
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

	cmd := exec.Command(bin, "--timeout", "0", "default", project, "--rox", filepath.Dir(truePath), "--", truePath)
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
