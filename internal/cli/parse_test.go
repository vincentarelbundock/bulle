package cli

import (
	"strings"
	"testing"
)

func TestParseRunWithProjectCommandAndFlags(t *testing.T) {
	opts, err := Parse([]string{
		"bulle", "--profile", "secrets", ".", "--rw", ".", "--ro", "~/.cache/uv,/tmp/cache",
		"--env", "PATH", "--env", "OPENAI_API_KEY", "--add-exec", "--add-libs",
		"--", "codex", "--model", "gpt-5",
	})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
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
	if !opts.AddExec || !opts.AddLibs {
		t.Fatalf("AddExec = %v, AddLibs = %v", opts.AddExec, opts.AddLibs)
	}
	if opts.NoWorkspace {
		t.Fatalf("NoWorkspace = true, want false")
	}
	if len(opts.Command) != 3 || opts.Command[0] != "codex" {
		t.Fatalf("Command = %#v", opts.Command)
	}
}

func TestParsePolicyFlag(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--policy", "--", "codex"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.Policy {
		t.Fatalf("Policy = false, want true")
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

func TestParseNoNetworkFlag(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--no-network", "--", "codex"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.NoNetwork {
		t.Fatalf("NoNetwork = false, want true")
	}
}

func TestParseProfileForms(t *testing.T) {
	for _, args := range [][]string{
		{"bulle", "-p", "codex", "."},
		{"bulle", "--profile=codex", "."},
	} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%#v) returned error: %v", args, err)
		}
		if opts.Profile != "codex" {
			t.Fatalf("Parse(%#v) Profile = %q, want codex", args, opts.Profile)
		}
		if opts.ProjectPath != "." {
			t.Fatalf("Parse(%#v) ProjectPath = %q, want .", args, opts.ProjectPath)
		}
	}
}

func TestParseDefaultsProjectPathToCurrentDirectory(t *testing.T) {
	for _, args := range [][]string{
		{"bulle"},
		{"bulle", "--profile", "codex"},
		{"bulle", "--profile", "codex", "--", "bash"},
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

func TestUsageShowsProfileShortFlag(t *testing.T) {
	if !strings.Contains(Usage(), "-p, --profile NAME") {
		t.Fatalf("Usage() does not show profile short flag:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "bulle [flags] [workspace]") {
		t.Fatalf("Usage() does not show optional workspace:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "--no-workspace") {
		t.Fatalf("Usage() does not show --no-workspace:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "--no-network") {
		t.Fatalf("Usage() does not show --no-network:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "macOS uses configured runtime roots") {
		t.Fatalf("Usage() does not explain macOS --add-libs behavior:\n%s", Usage())
	}
	for _, profile := range []string{"tool", "claude", "codex", "pi", "opencode"} {
		if !strings.Contains(Usage(), profile) {
			t.Fatalf("Usage() does not show built-in profile %q:\n%s", profile, Usage())
		}
	}
}

func TestReferenceMarkdownIncludesFullHelp(t *testing.T) {
	md := ReferenceMarkdown()
	if !strings.Contains(md, "bulle runs local coding agents inside a controlled workspace.") {
		t.Fatalf("ReferenceMarkdown does not include full Usage() text:\n%s", md)
	}
	if !strings.Contains(md, "Examples:") {
		t.Fatalf("ReferenceMarkdown does not include examples from Usage():\n%s", md)
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
