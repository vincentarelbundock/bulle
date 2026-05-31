//go:build linux

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func linuxROXPathArgs(paths ...string) []string {
	args := []string{}
	for _, path := range paths {
		args = append(args, "--rox", path)
	}
	return args
}

func linuxRuntimeROXPathArgs(extra ...string) []string {
	return linuxROXPathArgs(append([]string{"/bin", "/usr/bin", "/lib", "/lib64", "/usr/lib", "/usr/lib64"}, extra...)...)
}

func TestLinuxLandlockDeniesOutsideRead(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{project, "--rw", project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--env", "PATH", "--", "cat", outside)
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("cat outside succeeded, output: %s", string(out))
	}
}

func TestLinuxLandlockRunsScriptWithAddExecShebangInterpreter(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	script := filepath.Join(project, "hello")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf shebang-ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{project, "--add-exec"}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "./hello")
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v, output: %s", err, string(out))
	}
	if got := strings.TrimSpace(string(out)); got != "shebang-ok" {
		t.Fatalf("script output = %q, want shebang-ok", got)
	}
}

func TestLinuxLandlockClosesInheritedFileDescriptors(t *testing.T) {
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

	args := append([]string{project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "/bin/sh", "-c", "cat <&3")
	cmd := exec.Command(bin, args...)
	cmd.ExtraFiles = []*os.File{secret}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("inherited fd read succeeded, output: %s", string(out))
	}
	if strings.Contains(string(out), "secret-via-fd") {
		t.Fatalf("sandboxed command read inherited secret fd: %s", string(out))
	}
}

func TestLinuxLandlockRunsDynamicBinaryWithAddLibs(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	args := append([]string{project, "--add-libs"}, linuxROXPathArgs("/bin", "/usr/bin")...)
	args = append(args, "--", "/usr/bin/true")
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("true with add-libs failed: %v, output: %s", err, string(out))
	}
}

func TestLinuxLandlockRunsFromProjectPath(t *testing.T) {
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
	args := append([]string{project, "--rw", project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "/bin/pwd")
	cmd := exec.Command(bin, args...)
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
