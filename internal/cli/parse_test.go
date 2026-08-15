package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestParseRunWithProfileWorkspaceCommandAndFlags(t *testing.T) {
	opts, err := Parse([]string{
		"bulle", "secrets", ".", "--rw", ".", "--ro", "~/.cache/uv,/tmp/cache",
		"--env", "PATH", "--env", "OPENAI_API_KEY",
		"--", "codex", "--model", "gpt-5",
	})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if opts.Profile != "secrets" {
		t.Fatalf("Profile = %q, want secrets", opts.Profile)
	}
	if opts.ProjectPath != "." {
		t.Fatalf("ProjectPath = %q, want .", opts.ProjectPath)
	}
	if len(opts.ReadOnly) != 2 || opts.ReadOnly[0] != "~/.cache/uv" || opts.ReadOnly[1] != "/tmp/cache" {
		t.Fatalf("ReadOnly = %#v", opts.ReadOnly)
	}
	if len(opts.ReadWrite) != 1 || opts.ReadWrite[0] != "." {
		t.Fatalf("ReadWrite = %#v", opts.ReadWrite)
	}
	if len(opts.Env) != 2 || opts.Env[1] != "OPENAI_API_KEY" {
		t.Fatalf("Env = %#v", opts.Env)
	}
	if opts.NoWorkspace {
		t.Fatalf("NoWorkspace = true, want false")
	}
	if len(opts.Command) != 3 || opts.Command[0] != "codex" {
		t.Fatalf("Command = %#v", opts.Command)
	}
}

func TestParseRejectsRemovedFlags(t *testing.T) {
	// Former flags whose jobs moved elsewhere: profiles are the first
	// positional, executable discovery is automatic for explicit commands,
	// and the verbs are subcommands.
	for _, arg := range []string{"--profile", "--profile=claude", "-p", "--add-exec", "--add-libs", "--policy", "--last"} {
		if _, err := Parse([]string{"bulle", arg, "--", "true"}); err == nil {
			t.Fatalf("Parse(%q) succeeded, want unknown-flag error", arg)
		}
	}
}

func TestParseTimeoutFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want time.Duration
	}{
		{name: "space separated seconds", args: []string{"bulle", "--timeout", "30s", "--", "echo"}, want: 30 * time.Second},
		{name: "equals combined duration", args: []string{"bulle", "--timeout=1h30m", "--", "echo"}, want: 90 * time.Minute},
		{name: "omitted timeout", args: []string{"bulle", "--", "echo"}, want: 0},
		{name: "plain zero disables", args: []string{"bulle", "--timeout", "0", "--", "echo"}, want: 0},
		{name: "zero with unit disables", args: []string{"bulle", "--timeout", "0s", "--", "echo"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if opts.Timeout != tt.want {
				t.Fatalf("Timeout = %v, want %v", opts.Timeout, tt.want)
			}
		})
	}
}

func TestParseTimeoutRejectsInvalidValues(t *testing.T) {
	tests := []string{"30", "-1s", "ten seconds"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			_, err := Parse([]string{"bulle", "--timeout", value, "--", "echo"})
			if err == nil {
				t.Fatal("Parse returned nil error, want timeout validation error")
			}
			if !strings.Contains(err.Error(), `invalid --timeout value "`+value+`"`) {
				t.Fatalf("Parse error = %q, want invalid timeout value", err.Error())
			}
			if !strings.Contains(err.Error(), "30s") || !strings.Contains(err.Error(), "1h30m") {
				t.Fatalf("Parse error = %q, want Go duration examples", err.Error())
			}
		})
	}
}

func TestParseNoWorkspaceFlag(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--no-workspace", "--", "codex"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.NoWorkspace {
		t.Fatalf("NoWorkspace = false, want true")
	}
}

func TestParseRejectsNoNetworkFlag(t *testing.T) {
	_, err := Parse([]string{"bulle", "--no-network", "--", "codex"})
	if err == nil {
		t.Fatal("Parse returned nil error, want --no-network rejection")
	}
}

func TestParseProfilePositional(t *testing.T) {
	for _, tt := range []struct {
		args        []string
		wantProfile string
		wantPath    string
	}{
		{[]string{"bulle", "codex"}, "codex", "."},
		{[]string{"bulle", "codex", "."}, "codex", "."},
		{[]string{"bulle", "codex,offline", "/tmp"}, "codex,offline", "/tmp"},
	} {
		opts, err := Parse(tt.args)
		if err != nil {
			t.Fatalf("Parse(%#v) returned error: %v", tt.args, err)
		}
		if opts.Profile != tt.wantProfile {
			t.Fatalf("Parse(%#v) Profile = %q, want %q", tt.args, opts.Profile, tt.wantProfile)
		}
		if opts.ProjectPath != tt.wantPath {
			t.Fatalf("Parse(%#v) ProjectPath = %q, want %q", tt.args, opts.ProjectPath, tt.wantPath)
		}
	}
}

func TestParseDefaultsProjectPathToCurrentDirectory(t *testing.T) {
	for _, args := range [][]string{
		{"bulle"},
		{"bulle", "codex"},
		{"bulle", "codex", "--", "bash"},
	} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%#v) returned error: %v", args, err)
		}
		if opts.ProjectPath != "." {
			t.Fatalf("Parse(%#v) ProjectPath = %q, want .", args, opts.ProjectPath)
		}
	}
}

func TestProfileNamesSortsAlphabetically(t *testing.T) {
	cfg := config.Config{
		Profiles: map[string]config.Profile{
			"default":  {},
			"late":     {},
			"early":    {},
			"hidden":   {},
			"custom-b": {},
			"custom-a": {},
		},
	}

	got := ProfileNames(cfg)
	want := []string{"custom-a", "custom-b", "default", "early", "hidden", "late"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileNames = %#v, want %#v", got, want)
	}
}

func TestUsageShowsTheGrammarAndPointers(t *testing.T) {
	usage := Usage()
	for _, want := range []string{
		"bulle <profile>[,profile...] [dir] [-- command [args...]]",
		"Everything before -- is policy; everything after -- is the command.",
		"--ro PATH",
		"--env NAME[=VALUE]",
		"bulle scratch",
		"bulle show",
		"bulle profiles install SOURCE",
		"bulle completion bash|zsh|fish",
		"bulle help [grants|env|limits|config]",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("Usage() missing %q:\n%s", want, usage)
		}
	}
	for _, gone := range []string{"--profile", "--add-exec", "--add-libs", "rerun", "record", "--no-network"} {
		if strings.Contains(usage, gone) {
			t.Fatalf("Usage() still mentions %q:\n%s", gone, usage)
		}
	}
}

func TestHelpTopicsCoverAdvancedMaterial(t *testing.T) {
	for topic, want := range map[string]string{
		"grants": "which:NAME",
		"env":    "--env-file PATH",
		"limits": "--timeout DURATION",
		"config": "profiles/*.toml",
	} {
		text, ok := CommandHelp(topic)
		if !ok {
			t.Fatalf("help topic %q missing", topic)
		}
		if !strings.Contains(text, want) {
			t.Fatalf("help topic %q missing %q:\n%s", topic, want, text)
		}
	}
	topics := HelpTopics()
	if !reflect.DeepEqual(topics, []string{"config", "env", "grants", "limits"}) {
		t.Fatalf("HelpTopics = %#v", topics)
	}
}

func TestProfileListingShowsBuiltins(t *testing.T) {
	listing := ProfileListing(config.DefaultConfig())
	for _, profile := range []string{"tool", "network", "offline", "claude", "codex"} {
		if !strings.Contains(listing, profile) {
			t.Fatalf("ProfileListing missing built-in profile %q:\n%s", profile, listing)
		}
	}
}

func TestReferenceTypstIncludesFullHelp(t *testing.T) {
	page := ReferenceTypst()
	if !strings.Contains(page, "bulle runs coding agents and other dangerous tools inside a sandbox.") {
		t.Fatalf("ReferenceTypst does not include full Usage() text:\n%s", page)
	}
	if !strings.Contains(page, "<website-metadata>") {
		t.Fatalf("ReferenceTypst does not carry Calepin page metadata:\n%s", page)
	}
}

func TestParseHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{
		{"bulle", "help"},
		{"bulle", "--help"},
		{"bulle", "-h"},
		{"bulle", ".", "--help"},
	} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%#v) returned error: %v", args, err)
		}
		if !opts.Help {
			t.Fatalf("Parse(%#v) Help = false, want true", args)
		}
	}
	for _, args := range [][]string{
		{"bulle", "version"},
		{"bulle", "--version"},
		{"bulle", "-V"},
	} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%#v) returned error: %v", args, err)
		}
		if !opts.Version {
			t.Fatalf("Parse(%#v) Version = false, want true", args)
		}
	}
}

func TestParseScratchFlags(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--scratch", "--scratch-keep", "--", "claude"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.Scratch || !opts.ScratchKeep {
		t.Fatalf("Scratch = %v, ScratchKeep = %v; want both true", opts.Scratch, opts.ScratchKeep)
	}
}

func TestParseScratchRejectsValues(t *testing.T) {
	if _, err := Parse([]string{"bulle", "--scratch=worktree"}); err == nil || !strings.Contains(err.Error(), "worktree") {
		t.Fatalf("want worktree-specific error, got %v", err)
	}
	if _, err := Parse([]string{"bulle", "--scratch=clone"}); err == nil || !strings.Contains(err.Error(), "takes no value") {
		t.Fatalf("want no-value error, got %v", err)
	}
}
