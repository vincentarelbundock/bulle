package cli

import (
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/config"
)

var testCommands = []CommandSpec{
	{Name: "scratch", Verbs: []string{"list", "diff", "pull", "wipe", "shell"}},
	{Name: "record", Extra: []FlagSpec{
		{Name: "out", Short: "o", TakesValue: true, Complete: "file"},
		{Name: "max-rounds", TakesValue: true},
	}},
	{Name: "profiles", Verbs: []string{"list", "install"}},
	{Name: "policy"},
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
	if f := byName["profile"]; f.Short != "p" || !f.TakesValue || f.Complete != "profile" {
		t.Fatalf("profile spec wrong: %+v", f)
	}
	if f := byName["scratch"]; f.TakesValue {
		t.Fatalf("--scratch must not take a value: %+v", f)
	}
	if f := byName["ro"]; !f.TakesValue || f.Complete != "file" {
		t.Fatalf("ro spec wrong: %+v", f)
	}
}

func TestValueFlagsDerived(t *testing.T) {
	// The inference map must mirror the Flags struct exactly.
	for _, f := range GlobalFlags() {
		if valueFlags["--"+f.Name] != f.TakesValue {
			t.Errorf("valueFlags[--%s] = %v, struct says %v", f.Name, valueFlags["--"+f.Name], f.TakesValue)
		}
		if f.Short != "" && f.TakesValue && !valueFlags["-"+f.Short] {
			t.Errorf("short -%s missing from valueFlags", f.Short)
		}
	}
}

func TestCompleteSubcommands(t *testing.T) {
	cfg := config.DefaultConfig()
	got, directive := Complete(cfg, testCommands, []string{""})
	if !contains(names(got), "scratch") || !contains(names(got), "policy") {
		t.Fatalf("first-word completion missing subcommands: %v", got)
	}
	if contains(names(got), "__complete") {
		t.Fatalf("hidden command leaked into completion: %v", got)
	}
	if directive != DirectiveDefault {
		t.Fatalf("directive = %d, want default (workspace dirs complete too)", directive)
	}
}

func TestCompleteFlagNames(t *testing.T) {
	cfg := config.DefaultConfig()
	got, directive := Complete(cfg, testCommands, []string{"--pro"})
	if !contains(names(got), "--profile") {
		t.Fatalf("--pro did not complete to --profile: %v", got)
	}
	if directive != DirectiveNoFile {
		t.Fatalf("directive = %d, want nofile", directive)
	}
	// Subcommand-specific flags appear only under their subcommand.
	got, _ = Complete(cfg, testCommands, []string{"record", "--max"})
	if !contains(names(got), "--max-rounds") {
		t.Fatalf("record --max did not complete: %v", got)
	}
	got, _ = Complete(cfg, testCommands, []string{"--max"})
	if contains(names(got), "--max-rounds") {
		t.Fatalf("--max-rounds leaked outside record: %v", got)
	}
}

func TestCompleteProfileValues(t *testing.T) {
	cfg := config.DefaultConfig()
	got, directive := Complete(cfg, testCommands, []string{"--profile", ""})
	if !contains(names(got), "claude") {
		t.Fatalf("profile value completion missing claude: %v", got)
	}
	if directive != DirectiveNoFile {
		t.Fatalf("directive = %d, want nofile", directive)
	}
	// Comma-separated merges complete the last segment, keeping the head.
	got, _ = Complete(cfg, testCommands, []string{"--profile", "claude,off"})
	if !contains(names(got), "claude,offline") {
		t.Fatalf("comma merge completion wrong: %v", got)
	}
	// --profile=partial keeps the flag prefix.
	got, _ = Complete(cfg, testCommands, []string{"--profile=cla"})
	if !contains(names(got), "--profile=claude") {
		t.Fatalf("--profile= completion wrong: %v", got)
	}
}

func TestCompleteVerbsAndSeparator(t *testing.T) {
	cfg := config.DefaultConfig()
	got, _ := Complete(cfg, testCommands, []string{"scratch", "di"})
	if !contains(names(got), "diff") {
		t.Fatalf("scratch verb completion missing diff: %v", got)
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
