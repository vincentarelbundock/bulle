//go:build linux

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestLinuxOfflineProfileDeniesSocketCreation(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	probe := filepath.Join(t.TempDir(), "network-probe")
	buildLinuxNetworkProbe(t, probe)
	project := t.TempDir()

	cmd := exec.Command(bin, project, "--profile", "offline", "--add-exec", "--", probe)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("network probe succeeded with offline profile, output: %s", string(out))
	}
}

func buildLinuxNetworkProbe(t *testing.T, bin string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "main.go")
	code := `package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(13)
	}
	_ = syscall.Close(fd)
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build network probe: %v, output: %s", err, string(out))
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
		t.Fatalf("sleep succeeded, want timeout, output: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 124 {
		t.Fatalf("sleep exit = %v, want exit code 124, output: %s", err, string(out))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %v, want under 2s, output: %s", elapsed, string(out))
	}
	if !strings.Contains(string(out), "command timed out after 100ms") {
		t.Fatalf("timeout output = %q, want timeout message", string(out))
	}
}

func TestLinuxLandlockTimeoutKillsBackgroundChild(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()
	armed := filepath.Join(project, "armed")
	survived := filepath.Join(project, "survived")
	script := "(printf armed > " + shellQuote(armed) + "; sleep 1; printf survived > " + shellQuote(survived) + ") & wait"
	args := append([]string{"--timeout", "100ms", project, "--rw", project}, linuxRuntimeROXPathArgs()...)
	args = append(args, "--", "/bin/sh", "-c", script)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("shell succeeded, want timeout, output: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 124 {
		t.Fatalf("shell exit = %v, want exit code 124, output: %s", err, string(out))
	}
	if _, err := os.Stat(armed); err != nil {
		t.Fatalf("background child did not write setup marker: %v, output: %s", err, string(out))
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(survived); !os.IsNotExist(err) {
		t.Fatalf("background child wrote marker after timeout: %v, output: %s", err, string(out))
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
