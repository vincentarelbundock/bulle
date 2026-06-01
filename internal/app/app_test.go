package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/elfdeps"
)

func TestRunReturnsUsageErrorWhenCommandMissing(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr.String() == "" {
		t.Fatalf("stderr is empty")
	}
	if !bytes.Contains(stderr.Bytes(), []byte("no command supplied")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunDefaultsWorkspacePathToCurrentDirectory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "--profile", "tool", "--policy", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"workspace_path"`)) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunAddExecWithoutProjectGrantsCurrentDirectory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	code := Run([]string{"bulle", "--add-exec", "--policy", "--", "/bin/echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	want, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"rw":[`)) || !bytes.Contains(stdout.Bytes(), []byte(want)) {
		t.Fatalf("stdout = %s, want rw grant for %q", stdout.String(), want)
	}
}

func TestRunHelpPrintsUsage(t *testing.T) {
	for _, args := range [][]string{{"bulle", "--help"}, {"bulle", "help"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		code := Run(args, &stdout, &stderr)

		if code != 0 {
			t.Fatalf("Run(%#v) exit code = %d, stderr = %s", args, code, stderr.String())
		}
		if !bytes.Contains(stdout.Bytes(), []byte("Usage:")) {
			t.Fatalf("Run(%#v) stdout = %s", args, stdout.String())
		}
	}
}

func TestRunVersionPrintsVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "--version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("bulle ")) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestEnsureRuntimeDirsCreatesPrivateTmpOnly(t *testing.T) {
	tmp := t.TempDir()

	if err := ensureRuntimeDirs(tmp); err != nil {
		t.Fatalf("ensureRuntimeDirs returned error: %v", err)
	}
	for _, path := range []string{
		filepath.Join(tmp, "bulle"),
		filepath.Join(tmp, "bulle", "tmp"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", path)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %v, want 0700", path, info.Mode().Perm())
		}
	}
}

func TestEnsureRuntimeDirsRejectsSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	tmp := filepath.Join(base, "bulle-uid")
	target := t.TempDir()
	if err := os.Symlink(target, tmp); err != nil {
		t.Fatal(err)
	}

	if err := ensureRuntimeDirs(tmp); err == nil {
		t.Fatalf("ensureRuntimeDirs succeeded for symlinked runtime root")
	}
}

func TestRuntimeTempRootIsUserSpecific(t *testing.T) {
	base := t.TempDir()
	got := runtimeTempRoot(base)
	if filepath.Dir(got) != base {
		t.Fatalf("runtimeTempRoot(%q) = %q, want child of base", base, got)
	}
	if !bytes.Contains([]byte(filepath.Base(got)), []byte("bulle-")) {
		t.Fatalf("runtimeTempRoot(%q) = %q, want user-specific bulle directory", base, got)
	}
}

func TestRunExplainsBareCommandWithoutPolicyPATH(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "--", "ls"}, &stdout, &stderr)

	if code != ExitNotFound {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitNotFound, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("policy PATH")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("--env PATH")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunFindsBareCommandFromExplicitExecutableRoot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	tool := filepath.Join(binDir, "bulle-test-tool")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("not a script\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--rox", binDir, "--policy", "--", "bulle-test-tool"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(tool)) {
		t.Fatalf("stdout = %s, want command resolved to %q", stdout.String(), tool)
	}
	if bytes.Contains(stdout.Bytes(), []byte(`"PATH"`)) {
		t.Fatalf("stdout = %s, want executable root lookup without PATH env", stdout.String())
	}
}

func TestRunExplainsDefaultAppNotFoundBeforeSandbox(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfg, []byte(`
[profiles.missing]
inherits = "tool"
default_app = "definitely-not-installed-bulle-test-command"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", cfg, "--profile", "missing", tmp}, &stdout, &stderr)

	if code != ExitNotFound {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitNotFound, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("command not found before sandbox setup")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("definitely-not-installed-bulle-test-command")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunUsesDefaultAppFromConfig(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfg, []byte(`
default_app = "echo hi"
default_profile = "tool"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", cfg, tmp, "--policy"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`echo`)) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunUsesDefaultAppFromProfile(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfg, []byte(`
[profiles.default]
rw = ["$WORKSPACE"]
env = ["PATH"]

[profiles.agent]
inherits = "tool"
default_app = "echo profile"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", cfg, "--profile", "agent", tmp, "--policy"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`echo`)) || !bytes.Contains(stdout.Bytes(), []byte(`"profile"`)) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunParsesQuotedDefaultApp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfg, []byte(`
default_app = "echo 'hello world'"
default_profile = "tool"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", cfg, tmp, "--policy"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"hello world"`)) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunRejectsInvalidDefaultApp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfg, []byte(`
default_app = "echo 'unterminated"
default_profile = "tool"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", cfg, tmp, "--policy"}, &stdout, &stderr)

	if code != ExitConfigError {
		t.Fatalf("exit code = %d, want %d; stdout = %s; stderr = %s", code, ExitConfigError, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("invalid default_app")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunIgnoresProjectConfig(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	projectConfig := filepath.Join(tmp, ".bulle.toml")
	if err := os.WriteFile(projectConfig, []byte(`default_app = "echo from-project-config"`), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", tmp}, &stdout, &stderr)

	if code != ExitConfigError {
		t.Fatalf("exit code = %d, want %d; stdout = %s; stderr = %s", code, ExitConfigError, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("no command supplied")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("from-project-config")) || bytes.Contains(stderr.Bytes(), []byte("from-project-config")) {
		t.Fatalf("project config affected run; stdout = %s; stderr = %s", stdout.String(), stderr.String())
	}
}

func TestRunPolicyPrintsResolvedPolicy(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--profile", "tool", tmp, "--policy", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"workspace_path"`)) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"network":"full"`)) {
		t.Fatalf("stdout missing network mode: %s", stdout.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte(`"metadata"`)) {
		t.Fatalf("stdout looks like backend plan, want policy: %s", stdout.String())
	}
	if bytes.Contains(stderr.Bytes(), []byte("bulle profile")) {
		t.Fatalf("stderr contains profile summary during --policy output: %s", stderr.String())
	}
}

func TestRunPolicyIncludesNoNetwork(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--profile", "tool", "--no-network", tmp, "--policy", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"network":"none"`)) {
		t.Fatalf("stdout missing no-network policy: %s", stdout.String())
	}
}

func TestRunPolicyIncludesLinuxLibraryDepsWhenAddLibsIsSet(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ELF library discovery is Linux-specific")
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	deps, err := elfdeps.GetSystemLibraryDependencies("/usr/bin/true")
	if err != nil {
		t.Fatalf("GetLibraryDependencies returned error: %v", err)
	}
	if len(deps) == 0 {
		t.Skip("/usr/bin/true has no discoverable ELF dependencies")
	}

	code := Run([]string{"bulle", "--add-exec", "--add-libs", tmp, "--policy", "--", "/usr/bin/true"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	for _, dep := range deps {
		if !bytes.Contains(stdout.Bytes(), []byte(dep)) {
			t.Fatalf("stdout = %s, want resolved Linux library grant %q", stdout.String(), dep)
		}
	}
}
