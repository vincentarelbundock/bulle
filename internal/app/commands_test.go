package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/exitcode"
)

// The dispatch table and the completion protocol are exercised through Run,
// the same entry point the shell shims hit.

func runForTest(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"bulle"}, args...), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestCompleteEndpointListsSubcommandsAndProfiles(t *testing.T) {
	stdout, _, code := runForTest(t, "__complete", "--", "")
	if code != exitcode.OK {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	for _, want := range []string{"scratch", "show", "profiles", "completion", "claude", "offline"} {
		if !containsLine(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
	for _, gone := range []string{"__complete", "record", "rerun", "policy", "resolvers"} {
		if containsLine(stdout, gone) {
			t.Errorf("%q leaked into completion: %s", gone, stdout)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(stdout, "\n"), ":4") {
		t.Errorf("first word should suppress file completion: %q", stdout)
	}
}

func TestCompletionScriptsPrint(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		stdout, _, code := runForTest(t, "completion", shell)
		if code != exitcode.OK || !strings.Contains(stdout, "__complete") {
			t.Errorf("completion %s: exit %d, output %q", shell, code, stdout)
		}
	}
	_, stderr, code := runForTest(t, "completion", "tcsh")
	if code != exitcode.ConfigError || !strings.Contains(stderr, "bash, zsh, or fish") {
		t.Errorf("bad shell: exit %d, stderr %q", code, stderr)
	}
	// Bare invocation of an argument-requiring subcommand shows its help.
	stdout, _, code := runForTest(t, "completion")
	if code != exitcode.OK || !strings.Contains(stdout, "bulle completion bash|zsh|fish") {
		t.Errorf("bare completion: exit %d, stdout %q", code, stdout)
	}
}

func TestSubcommandHelp(t *testing.T) {
	// Every dispatched subcommand must have a help topic, so `bulle help
	// <name>` and `--help` cannot drift from the dispatch table.
	for _, sub := range subcommands() {
		if sub.Hidden {
			continue
		}
		stdout, _, code := runForTest(t, "help", sub.Name)
		if code != exitcode.OK || !strings.Contains(stdout, "Usage:") {
			t.Errorf("help %s: exit %d, stdout %q", sub.Name, code, stdout)
		}
	}
	stdout, _, code := runForTest(t, "scratch", "--help")
	if code != exitcode.OK || !strings.Contains(stdout, "disposable") {
		t.Errorf("scratch --help: exit %d", code)
	}
	stdout, _, code = runForTest(t, "show", "-h")
	if code != exitcode.OK || !strings.Contains(stdout, "--json") {
		t.Errorf("show -h: exit %d, stdout %q", code, stdout)
	}
	stdout, _, code = runForTest(t, "profiles")
	if code != exitcode.OK || !strings.Contains(stdout, "Usage:") {
		t.Errorf("bare profiles: exit %d, stdout %q", code, stdout)
	}
	_, stderr, code := runForTest(t, "help", "bogus")
	if code != exitcode.ConfigError || !strings.Contains(stderr, "topics:") {
		t.Errorf("help bogus: exit %d, stderr %q", code, stderr)
	}
}

func TestHelpTopicsDispatch(t *testing.T) {
	for topic, want := range map[string]string{
		"grants": "which:NAME",
		"env":    "--env-file",
		"limits": "--timeout",
		"config": "profiles/",
	} {
		stdout, _, code := runForTest(t, "help", topic)
		if code != exitcode.OK || !strings.Contains(stdout, want) {
			t.Errorf("help %s: exit %d, stdout %q", topic, code, stdout)
		}
	}
}

func TestBareBulleShowsHelp(t *testing.T) {
	// Isolate from the machine's user config so no default_app can turn a
	// bare invocation into a real sandboxed run.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stdout, _, code := runForTest(t)
	if code != exitcode.OK || !strings.Contains(stdout, "bulle runs coding agents") {
		t.Errorf("bare bulle: exit %d, stdout %q", code, stdout)
	}
}

func TestDirectoryInProfileSlotIsExplained(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, stderr, code := runForTest(t, ".")
	if code != exitcode.ConfigError || !strings.Contains(stderr, "is a directory, not a profile") {
		t.Errorf("bulle .: exit %d, stderr %q", code, stderr)
	}
}

func TestCommandInProfileSlotIsExplained(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// A name that is on PATH but is not a profile gets pointed at the -- form.
	_, stderr, code := runForTest(t, "ls")
	if code != exitcode.ConfigError || !strings.Contains(stderr, "bulle -- ls") {
		t.Errorf("bulle ls: exit %d, stderr %q", code, stderr)
	}
}

func TestSupportProfileWithoutCommandIsExplained(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, stderr, code := runForTest(t, "network")
	if code != exitcode.ConfigError || !strings.Contains(stderr, "has no default app") {
		t.Errorf("bulle network: exit %d, stderr %q", code, stderr)
	}
}

func TestWantsHelpStopsAtSeparator(t *testing.T) {
	if !wantsHelp([]string{"tool", "--help"}) {
		t.Error("--help before -- not detected")
	}
	if wantsHelp([]string{"--", "tool", "--help"}) {
		t.Error("--help after -- must belong to the sandboxed command")
	}
}

func TestHelpAndVersionDispatch(t *testing.T) {
	stdout, _, code := runForTest(t, "help")
	if code != exitcode.OK || !strings.Contains(stdout, "bulle runs coding agents") {
		t.Errorf("help: exit %d", code)
	}
	if !strings.Contains(stdout, "bulle completion bash|zsh|fish") {
		t.Errorf("help does not document completion")
	}
	stdout, _, code = runForTest(t, "version")
	if code != exitcode.OK || !strings.HasPrefix(stdout, "bulle ") {
		t.Errorf("version: exit %d, output %q", code, stdout)
	}
}

func containsLine(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		name, _, _ := strings.Cut(line, "\t")
		if name == want {
			return true
		}
	}
	return false
}

func TestShowProfilesListsBuiltins(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stdout, _, code := runForTest(t, "show", "profiles")
	if code != exitcode.OK {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	for _, want := range []string{"claude", "offline", "tool"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show profiles missing %q: %s", want, stdout)
		}
	}
}

func TestShowResolversLists(t *testing.T) {
	stdout, _, code := runForTest(t, "show", "resolvers")
	if code != exitcode.OK || stdout == "" {
		t.Errorf("show resolvers: exit %d, stdout %q", code, stdout)
	}
}

// The configuration root is named explicitly rather than through
// XDG_CONFIG_HOME: only Linux derives the root from XDG, so setting that
// variable tests the platform rather than the reporting.
func TestShowConfigReportsStatus(t *testing.T) {
	dir := t.TempDir()
	stdout, _, code := runForTest(t, "show", "config", "--config", dir)
	if code != exitcode.OK {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	for _, want := range []string{"configuration root:", "config.toml: not found", "profiles/:   not found", "built-in profiles:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
	// A broken config.toml becomes visible here, unlike during runs.
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = runForTest(t, "show", "config", "--config", dir)
	if code != exitcode.ConfigError || !strings.Contains(stdout, "config.toml: ERROR") {
		t.Errorf("broken config: exit %d, stdout %q", code, stdout)
	}
}

func TestProfileTypoSuggestion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, stderr, code := runForTest(t, "show", "claud")
	if code == exitcode.OK || !strings.Contains(stderr, `did you mean claude?`) {
		t.Errorf("typo'd profile: exit %d, stderr %q", code, stderr)
	}
}
