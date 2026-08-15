package cli

import (
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/config"
)

var testCommands = []CommandSpec{
	{Name: "scratch", Verbs: []string{"list", "diff", "pull", "wipe", "shell"}},
	{Name: "show", Verbs: []string{"policy", "profiles", "resolvers", "config"}, Extra: []FlagSpec{
		{Name: "json"},
	}},
	{Name: "profiles", Verbs: []string{"list", "install"}},
	{Name: "__complete", Hidden: true},
}

func names(candidates []string) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i], _, _ = strings.Cut(c, "\t")
	}
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestGlobalFlagsDeriveFromStruct(t *testing.T) {
	flags := GlobalFlags()
	byName := map[string]FlagSpec{}
	for _, f := range flags {
		byName[f.Name] = f
	}
	if _, ok := byName["profile"]; ok {
		t.Fatalf("--profile is gone; the profile is the first positional")
	}
	if f := byName["scratch"]; f.TakesValue {
		t.Fatalf("--scratch must not take a value: %+v", f)
	}
	if f := byName["ro"]; !f.TakesValue || f.Complete != "file" {
		t.Fatalf("ro spec wrong: %+v", f)
	}
}

func TestCompleteFirstWordOffersCommandsAndProfiles(t *testing.T) {
	cfg := config.DefaultConfig()
	got, directive := Complete(cfg, testCommands, []string{""})
	if !contains(names(got), "scratch") || !contains(names(got), "show") {
		t.Fatalf("first-word completion missing subcommands: %v", got)
	}
	if !contains(names(got), "claude") || !contains(names(got), "offline") {
		t.Fatalf("first-word completion missing profiles: %v", got)
	}
	if contains(names(got), "__complete") {
		t.Fatalf("hidden command leaked into completion: %v", got)
	}
	if directive != DirectiveNoFile {
		t.Fatalf("directive = %d, want nofile (position 1 is never a file)", directive)
	}
	// Comma-separated merges complete the last segment, keeping the head.
	got, _ = Complete(cfg, testCommands, []string{"claude,off"})
	if !contains(names(got), "claude,offline") {
		t.Fatalf("comma merge completion wrong: %v", got)
	}
}

func TestCompleteSecondPositionalIsADirectory(t *testing.T) {
	cfg := config.DefaultConfig()
	got, directive := Complete(cfg, testCommands, []string{"claude", ""})
	if len(got) != 0 || directive != DirectiveDefault {
		t.Fatalf("workspace slot = %v, %d; want shell file completion", got, directive)
	}
}

func TestCompleteFlagNames(t *testing.T) {
	cfg := config.DefaultConfig()
	got, directive := Complete(cfg, testCommands, []string{"--r"})
	if !contains(names(got), "--ro") || !contains(names(got), "--rwx") {
		t.Fatalf("--r did not complete grant flags: %v", got)
	}
	if directive != DirectiveNoFile {
		t.Fatalf("directive = %d, want nofile", directive)
	}
	// Subcommand-specific flags appear only under their subcommand.
	got, _ = Complete(cfg, testCommands, []string{"show", "--js"})
	if !contains(names(got), "--json") {
		t.Fatalf("show --js did not complete: %v", got)
	}
	got, _ = Complete(cfg, testCommands, []string{"--js"})
	if contains(names(got), "--json") {
		t.Fatalf("--json leaked outside show: %v", got)
	}
}

func TestCompleteVerbsAndSeparator(t *testing.T) {
	cfg := config.DefaultConfig()
	got, _ := Complete(cfg, testCommands, []string{"scratch", "di"})
	if !contains(names(got), "diff") {
		t.Fatalf("scratch verb completion missing diff: %v", got)
	}
	// scratch also starts runs, so the profile slot completes there too.
	got, _ = Complete(cfg, testCommands, []string{"scratch", "cla"})
	if !contains(names(got), "claude") {
		t.Fatalf("scratch profile completion missing claude: %v", got)
	}
	// After a chosen verb, no more verbs.
	got, _ = Complete(cfg, testCommands, []string{"scratch", "diff", ""})
	if contains(names(got), "pull") {
		t.Fatalf("verbs offered after one was chosen: %v", got)
	}
	// Everything after -- belongs to the sandboxed command.
	got, directive := Complete(cfg, testCommands, []string{"--", "gi"})
	if len(got) != 0 || directive != DirectiveDefault {
		t.Fatalf("post-separator completion = %v, %d; want none, default", got, directive)
	}
	// A path value delegates to the shell.
	got, directive = Complete(cfg, testCommands, []string{"--ro", ""})
	if len(got) != 0 || directive != DirectiveDefault {
		t.Fatalf("--ro value = %v, %d; want none, default", got, directive)
	}
}

func TestCompletionScriptShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := CompletionScript(shell)
		if err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		if !strings.Contains(script, "__complete") {
			t.Fatalf("%s shim does not call __complete", shell)
		}
	}
	if _, err := CompletionScript("powershell"); err == nil {
		t.Fatal("unsupported shell accepted")
	}
}
