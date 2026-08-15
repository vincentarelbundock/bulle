package app

import (
	"bytes"
	"strings"
	"testing"
)

// The dispatch table and the completion protocol are exercised through Run,
// the same entry point the shell shims hit.

func runForTest(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"bulle"}, args...), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestCompleteEndpointListsSubcommands(t *testing.T) {
	stdout, _, code := runForTest(t, "__complete", "--", "")
	if code != ExitOK {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	for _, want := range []string{"scratch", "record", "profiles", "policy", "rerun", "completion"} {
		if !containsLine(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
	if containsLine(stdout, "__complete") {
		t.Errorf("hidden command leaked: %s", stdout)
	}
	if !strings.HasSuffix(strings.TrimRight(stdout, "\n"), ":0") {
		t.Errorf("missing default directive: %q", stdout)
	}
}

func TestCompleteEndpointProfiles(t *testing.T) {
	stdout, _, code := runForTest(t, "__complete", "--", "--profile", "")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !containsLine(stdout, "claude") {
		t.Errorf("profile completion missing claude: %s", stdout)
	}
	if !strings.HasSuffix(strings.TrimRight(stdout, "\n"), ":4") {
		t.Errorf("profile values should suppress file completion: %q", stdout)
	}
}

func TestCompletionScriptsPrint(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		stdout, _, code := runForTest(t, "completion", shell)
		if code != ExitOK || !strings.Contains(stdout, "__complete") {
			t.Errorf("completion %s: exit %d, output %q", shell, code, stdout)
		}
	}
	_, stderr, code := runForTest(t, "completion", "tcsh")
	if code != ExitConfigError || !strings.Contains(stderr, "bash, zsh, or fish") {
		t.Errorf("bad shell: exit %d, stderr %q", code, stderr)
	}
	// Bare invocation of an argument-requiring subcommand shows its help.
	stdout, _, code := runForTest(t, "completion")
	if code != ExitOK || !strings.Contains(stdout, "bulle completion bash|zsh|fish") {
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
		if code != ExitOK || !strings.Contains(stdout, "Usage:") {
			t.Errorf("help %s: exit %d, stdout %q", sub.Name, code, stdout)
		}
	}
	stdout, _, code := runForTest(t, "scratch", "--help")
	if code != ExitOK || !strings.Contains(stdout, "disposable") {
		t.Errorf("scratch --help: exit %d", code)
	}
	stdout, _, code = runForTest(t, "policy", "-h")
	if code != ExitOK || !strings.Contains(stdout, "--json") {
		t.Errorf("policy -h: exit %d, stdout %q", code, stdout)
	}
	for _, args := range [][]string{{"profiles"}, {"record"}} {
		stdout, _, code = runForTest(t, args...)
		if code != ExitOK || !strings.Contains(stdout, "Usage:") {
			t.Errorf("bare %v: exit %d, stdout %q", args, code, stdout)
		}
	}
	_, stderr, code := runForTest(t, "help", "bogus")
	if code != ExitConfigError || !strings.Contains(stderr, "topics:") {
		t.Errorf("help bogus: exit %d, stderr %q", code, stderr)
	}
}

func TestBareBulleShowsHelp(t *testing.T) {
	// Isolate from the machine's user config so no default_app can turn a
	// bare invocation into a real sandboxed run.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stdout, _, code := runForTest(t)
	if code != ExitOK || !strings.Contains(stdout, "bulle runs local coding agents") {
		t.Errorf("bare bulle: exit %d, stdout %q", code, stdout)
	}
	// A workspace argument without a command keeps the explicit error.
	_, stderr, code := runForTest(t, ".")
	if code != ExitConfigError || !strings.Contains(stderr, "no command supplied") {
		t.Errorf("bulle .: exit %d, stderr %q", code, stderr)
	}
}

func TestWantsHelpStopsAtSeparator(t *testing.T) {
	if !wantsHelp([]string{"--profile", "go", "--help"}) {
		t.Error("--help before -- not detected")
	}
	if wantsHelp([]string{"--", "tool", "--help"}) {
		t.Error("--help after -- must belong to the sandboxed command")
	}
}

func TestHelpAndVersionDispatch(t *testing.T) {
	stdout, _, code := runForTest(t, "help")
	if code != ExitOK || !strings.Contains(stdout, "bulle runs local coding agents") {
		t.Errorf("help: exit %d", code)
	}
	if !strings.Contains(stdout, "bulle completion bash|zsh|fish") {
		t.Errorf("help does not document completion")
	}
	stdout, _, code = runForTest(t, "version")
	if code != ExitOK || !strings.HasPrefix(stdout, "bulle ") {
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
