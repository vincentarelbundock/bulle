package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/elfdeps"
	"github.com/vincentarelbundock/bulle/internal/policy"
	"github.com/vincentarelbundock/bulle/internal/supervisor"
)

func TestRunDefinesTimeoutExitCode(t *testing.T) {
	if ExitTimedOut != 124 {
		t.Fatalf("ExitTimedOut = %d, want 124", ExitTimedOut)
	}
}

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

func TestRunPreparedPolicyRejectsMissingFD(t *testing.T) {
	var stderr bytes.Buffer

	code := runPreparedPolicy([]string{"bulle", "__run-prepared-policy"}, &stderr)

	if code != ExitSandboxSetup {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitSandboxSetup, stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("usage: bulle __run-prepared-policy --policy-fd FD")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunDoesNotTreatIncompletePreparedPolicyInvocationAsRunner(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "__run-prepared-policy", "--policy-fd"}, &stdout, &stderr)

	if code != ExitConfigError {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitConfigError, stderr.String())
	}
	if bytes.Contains(stderr.Bytes(), []byte("usage: bulle __run-prepared-policy --policy-fd FD")) {
		t.Fatalf("public CLI invocation was handled as hidden runner: %s", stderr.String())
	}
}

func TestRunPreparedPolicyDoesNotShadowWorkspacePath(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, preparedPolicyRunnerCommand)
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	truePath := "/bin/true"
	if _, err := os.Stat(truePath); err != nil {
		truePath = "/usr/bin/true"
	}
	truePath, err = filepath.EvalSymlinks(truePath)
	if err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", preparedPolicyRunnerCommand, "--rox", filepath.Dir(truePath), "--policy=summary", "--", truePath}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitOK, stderr.String())
	}
	if bytes.Contains(stderr.Bytes(), []byte("usage: bulle __run-prepared-policy --policy-fd FD")) {
		t.Fatalf("workspace path was handled as hidden runner: %s", stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("bulle profile")) {
		t.Fatalf("stdout = %s", stdout.String())
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

func TestRunPreparedPolicyClosesFDBeforeBackend(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	fd := writePreparedPolicy(t, policy.Policy{Backend: policy.BackendName("test")})

	called := false
	oldRunPreparedPolicyBackend := runPreparedPolicyBackend
	runPreparedPolicyBackend = func(policy.Policy, io.Writer) int {
		called = true
		dup, err := syscall.Dup(fd)
		if err != syscall.EBADF {
			if err == nil {
				_ = syscall.Close(dup)
			}
			t.Fatalf("policy fd is still open when backend starts; err = %v", err)
		}
		return ExitOK
	}
	defer func() {
		runPreparedPolicyBackend = oldRunPreparedPolicyBackend
	}()

	code := Run([]string{"bulle", "__run-prepared-policy", "--policy-fd", strconv.Itoa(fd)}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, ExitOK, stderr.String())
	}
	if !called {
		t.Fatal("backend was not called")
	}
}

func writePreparedPolicy(t *testing.T, p policy.Policy) int {
	t.Helper()

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "prepared-policy.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

func TestRunDefaultsWorkspacePathToCurrentDirectory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "--profile", "tool", "--policy=json", "--", "echo", "hi"}, &stdout, &stderr)

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

	code := Run([]string{"bulle", "--add-exec", "--policy=json", "--", "/bin/echo", "hi"}, &stdout, &stderr)

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

func TestRunListProfilesPrintsBuiltInProfiles(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"bulle", "--list-profiles"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	for _, want := range []string{"tool\n", "network\n", "offline\n", "macos-dns\n", "macos-certs\n", "keychain\n", "claude\n", "codex\n", "pi\n", "opencode\n"} {
		if !bytes.Contains(stdout.Bytes(), []byte(want)) {
			t.Fatalf("stdout = %s, want profile %q", stdout.String(), want)
		}
	}
	if !bytes.Contains(stdout.Bytes(), []byte("default\n")) {
		t.Fatalf("stdout = %s, want default profile listed", stdout.String())
	}
}

func TestRunListProfilesIncludesConfigProfiles(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()
	writeConfigProfile(t, tmp, "custom", `inherits = "tool"
`)

	code := Run([]string{"bulle", "--config", tmp, "--list-profiles"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("custom\n")) {
		t.Fatalf("stdout = %s, want custom profile", stdout.String())
	}
}

func TestRunInstallProfilesCopiesSingleProfileFile(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	configRoot := t.TempDir()
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "agent.toml")
	data := "title = \"Agent\"\ninherits = \"tool\"\n"
	if err := os.WriteFile(source, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", configRoot, "--install-profiles", source}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	dest := filepath.Join(configRoot, "profiles", "agent.toml")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read installed profile: %v", err)
	}
	if string(got) != data {
		t.Fatalf("installed profile = %q, want %q", string(got), data)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("installed agent\n")) {
		t.Fatalf("stdout = %s, want installed agent", stdout.String())
	}
}

func TestRunInstallProfilesCopiesDirectoryTomlFiles(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	configRoot := t.TempDir()
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "one.toml"), []byte("title = \"One\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "two.toml"), []byte("title = \"Two\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("not a profile"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", configRoot, "--install-profiles", sourceDir}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	for _, name := range []string{"one", "two"} {
		if _, err := os.Stat(filepath.Join(configRoot, "profiles", name+".toml")); err != nil {
			t.Fatalf("installed %s: %v", name, err)
		}
	}
}

func TestRunInstallProfilesUsesProfilesDirectoryInRepository(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	configRoot := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	profiles := filepath.Join(repo, "profiles")
	if err := os.Mkdir(profiles, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "repo-agent.toml"), []byte("title = \"Repo Agent\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.toml"), []byte("title = \"Ignored\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", configRoot, "--install-profiles", repo}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(configRoot, "profiles", "repo-agent.toml")); err != nil {
		t.Fatalf("installed repo-agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "profiles", "ignored.toml")); !os.IsNotExist(err) {
		t.Fatalf("ignored root profile err = %v, want not installed", err)
	}
}

func TestRunInstallProfilesDoesNotPartiallyCopyInvalidDirectory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	configRoot := t.TempDir()
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "good.toml"), []byte("title = \"Good\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "zz-bad.toml"), []byte("unknown = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"bulle", "--config", configRoot, "--install-profiles", sourceDir}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("exit code = 0, want install failure")
	}
	if _, err := os.Stat(filepath.Join(configRoot, "profiles", "good.toml")); !os.IsNotExist(err) {
		t.Fatalf("good profile err = %v, want no partial install", err)
	}
}

func TestParseGitHubProfileInstallSourceWithSubdirectory(t *testing.T) {
	repo, subdir, ok := parseGitHubProfileInstallSource("github:vincentarelbundock/bulle/custom_profiles")
	if !ok {
		t.Fatal("parseGitHubProfileInstallSource returned ok=false")
	}
	if repo != "https://github.com/vincentarelbundock/bulle.git" {
		t.Fatalf("repo = %q", repo)
	}
	if subdir != "custom_profiles" {
		t.Fatalf("subdir = %q", subdir)
	}
}

func TestParseGitHubProfileInstallSourceRequiresPrefix(t *testing.T) {
	for _, source := range []string{"vincentarelbundock/bulle/custom_profiles", "https://github.com/vincentarelbundock/bulle.git"} {
		if _, _, ok := parseGitHubProfileInstallSource(source); ok {
			t.Fatalf("parseGitHubProfileInstallSource(%q) ok=true, want false", source)
		}
	}
}

func writeConfigProfile(t *testing.T, root string, name string, data string) {
	t.Helper()
	profileDir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, name+".toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
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
	writeConfigProfile(t, tmp, "missing", `inherits = "tool"
default_app = "definitely-not-installed-bulle-test-command"
`)

	code := Run([]string{"bulle", "--config", tmp, "--profile", "missing", tmp}, &stdout, &stderr)

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
	writeConfigProfile(t, tmp, "default", `default_app = "/bin/echo hi"
rox = ["/bin"]
`)

	code := Run([]string{"bulle", "--config", tmp, tmp, "--policy=json"}, &stdout, &stderr)

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
	writeConfigProfile(t, tmp, "agent", `inherits = "tool"
default_app = "echo profile"
`)

	code := Run([]string{"bulle", "--config", tmp, "--profile", "agent", tmp, "--policy=json"}, &stdout, &stderr)

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
	writeConfigProfile(t, tmp, "default", `default_app = "/bin/echo 'hello world'"
rox = ["/bin"]
`)

	code := Run([]string{"bulle", "--config", tmp, tmp, "--policy=json"}, &stdout, &stderr)

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
	writeConfigProfile(t, tmp, "default", `default_app = "echo 'unterminated"
`)

	code := Run([]string{"bulle", "--config", tmp, tmp, "--policy=json"}, &stdout, &stderr)

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

func TestRunPolicyJSONPrintsResolvedPolicy(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--profile", "tool", tmp, "--policy=json", "--", "echo", "hi"}, &stdout, &stderr)

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

func TestRunPolicySummaryIncludesTimeoutWhenConfigured(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--timeout", "250ms", "--profile", "tool", tmp, "--policy", "--", "echo", "hi"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("  timeout: 250ms\n")) {
		t.Fatalf("stdout missing timeout: %s", stdout.String())
	}
}

func TestRunPolicyPrintsHumanReadableSummaryByDefault(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{
		"bulle",
		"--profile", "tool",
		"--env", "BULLE_TEST_SECRET=super-secret-value",
		tmp,
		"--policy",
		"--", "/bin/echo", "hi",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`bulle profile "tool" permissions:`)) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("  filesystem:\n")) {
		t.Fatalf("stdout missing filesystem section: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("  environment: BULLE_TEST_SECRET, BUN_TMPDIR, PATH, TEMP, TMP, TMPDIR\n")) {
		t.Fatalf("stdout missing environment summary: %s", stdout.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("super-secret-value")) {
		t.Fatalf("stdout leaked secret value: %s", stdout.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte(`"workspace_path"`)) {
		t.Fatalf("stdout contains JSON policy: %s", stdout.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("\nhi\n")) {
		t.Fatalf("stdout contains command output: %s", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExitCodeForSupervisorTimeoutError(t *testing.T) {
	var stderr bytes.Buffer

	code := exitCodeForSupervisorError(&supervisor.TimeoutError{Duration: 250 * time.Millisecond}, &stderr)

	if code != ExitTimedOut {
		t.Fatalf("exit code = %d, want %d", code, ExitTimedOut)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("bulle: command timed out after 250ms\n")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExitCodeForSupervisorTimeoutWithTerminalRestoreError(t *testing.T) {
	var stderr bytes.Buffer
	err := errors.Join(
		&supervisor.TimeoutError{Duration: 250 * time.Millisecond},
		&supervisor.TerminalRestoreError{Err: errors.New("restore failed")},
	)

	code := exitCodeForSupervisorError(err, &stderr)

	if code != ExitTimedOut {
		t.Fatalf("exit code = %d, want %d", code, ExitTimedOut)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("bulle: command timed out after 250ms\n")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("restore failed\n")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExitCodeForSupervisorExitError(t *testing.T) {
	var stderr bytes.Buffer

	code := exitCodeForSupervisorError(&supervisor.ExitError{Code: 7}, &stderr)

	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExitCodeForSupervisorZeroExitError(t *testing.T) {
	var stderr bytes.Buffer

	code := exitCodeForSupervisorError(&supervisor.ExitError{Code: 0}, &stderr)

	if code != ExitCommandFailed {
		t.Fatalf("exit code = %d, want %d", code, ExitCommandFailed)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExitCodeForSupervisorExitWithTerminalRestoreError(t *testing.T) {
	var stderr bytes.Buffer
	err := errors.Join(
		&supervisor.ExitError{Code: 7},
		&supervisor.TerminalRestoreError{Err: errors.New("restore failed")},
	)

	code := exitCodeForSupervisorError(err, &stderr)

	if code != ExitSandboxSetup {
		t.Fatalf("exit code = %d, want %d", code, ExitSandboxSetup)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("restore failed\n")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExitCodeForSupervisorGenericError(t *testing.T) {
	var stderr bytes.Buffer

	code := exitCodeForSupervisorError(errors.New("setup failed"), &stderr)

	if code != ExitSandboxSetup {
		t.Fatalf("exit code = %d, want %d", code, ExitSandboxSetup)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("setup failed\n")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRunPolicyIncludesOfflineProfileOverlay(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	tmp := t.TempDir()

	code := Run([]string{"bulle", "--profile", "tool,offline", tmp, "--policy=json", "--", "echo", "hi"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"network":"none"`)) {
		t.Fatalf("stdout missing offline policy: %s", stdout.String())
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

	code := Run([]string{"bulle", "--add-exec", "--add-libs", tmp, "--policy=json", "--", "/usr/bin/true"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	for _, dep := range deps {
		if !bytes.Contains(stdout.Bytes(), []byte(dep)) {
			t.Fatalf("stdout = %s, want resolved Linux library grant %q", stdout.String(), dep)
		}
	}
}
