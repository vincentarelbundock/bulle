package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/config"
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

func TestParsePolicyFlagDefaultsToSummary(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--policy", "--", "codex"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.Policy {
		t.Fatalf("Policy = false, want true")
	}
	if opts.PolicyFormat != "summary" {
		t.Fatalf("PolicyFormat = %q, want summary", opts.PolicyFormat)
	}
	if len(opts.Command) != 1 || opts.Command[0] != "codex" {
		t.Fatalf("Command = %#v, want [codex]", opts.Command)
	}
}

func TestParsePolicyFormatValues(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want string
	}{
		{arg: "--policy=summary", want: "summary"},
		{arg: "--policy=json", want: "json"},
	} {
		opts, err := Parse([]string{"bulle", tc.arg, "--", "codex", "--model", "gpt-5"})
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", tc.arg, err)
		}
		if !opts.Policy {
			t.Fatalf("Parse(%q) Policy = false, want true", tc.arg)
		}
		if opts.PolicyFormat != tc.want {
			t.Fatalf("Parse(%q) PolicyFormat = %q, want %q", tc.arg, opts.PolicyFormat, tc.want)
		}
		if len(opts.Command) != 3 || opts.Command[0] != "codex" || opts.Command[1] != "--model" || opts.Command[2] != "gpt-5" {
			t.Fatalf("Parse(%q) Command = %#v, want [codex --model gpt-5]", tc.arg, opts.Command)
		}
	}
}

func TestParsePolicyRejectsInvalidFormat(t *testing.T) {
	_, err := Parse([]string{"bulle", "--policy=pretty"})
	if err == nil {
		t.Fatal("Parse returned nil error, want invalid --policy value error")
	}
	if !strings.Contains(err.Error(), `invalid --policy value "pretty"`) {
		t.Fatalf("Parse error = %q, want invalid --policy value pretty", err.Error())
	}
}

func TestParsePolicyRejectsConflictingFormats(t *testing.T) {
	_, err := Parse([]string{"bulle", "--policy=json", "--policy=summary"})
	if err == nil {
		t.Fatal("Parse returned nil error, want conflicting --policy values error")
	}
	if !strings.Contains(err.Error(), "conflicting --policy values") {
		t.Fatalf("Parse error = %q, want conflicting --policy values", err.Error())
	}
}

func TestParseListProfilesFlag(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--list-profiles"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !opts.ListProfiles {
		t.Fatalf("ListProfiles = false, want true")
	}
}

func TestParseInstallProfilesFlag(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--install-profiles", "profiles"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if opts.InstallProfiles != "profiles" {
		t.Fatalf("InstallProfiles = %q, want profiles", opts.InstallProfiles)
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

func TestParseProfileForms(t *testing.T) {
	for _, args := range [][]string{
		{"bulle", "-p", "codex", "."},
		{"bulle", "--profile=codex", "."},
		{"bulle", "--profile", "codex,offline", "."},
	} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%#v) returned error: %v", args, err)
		}
		wantProfile := "codex"
		if strings.Contains(args[1], ",") || len(args) > 2 && strings.Contains(args[2], ",") {
			wantProfile = "codex,offline"
		}
		if opts.Profile != wantProfile {
			t.Fatalf("Parse(%#v) Profile = %q, want %q", args, opts.Profile, wantProfile)
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
	if strings.Contains(Usage(), "--no-network") {
		t.Fatalf("Usage() still shows --no-network:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "--list-profiles") {
		t.Fatalf("Usage() does not show --list-profiles:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "--install-profiles SOURCE") {
		t.Fatalf("Usage() does not show --install-profiles:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "--timeout DURATION") {
		t.Fatalf("Usage() does not show --timeout:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "Go duration") {
		t.Fatalf("Usage() does not explain Go duration syntax:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "exit 124") {
		t.Fatalf("Usage() does not document timeout exit code:\n%s", Usage())
	}
	for _, example := range []string{
		"bulle --install-profiles agent.toml",
		"bulle --install-profiles ./profiles",
		"bulle --install-profiles github:vincentarelbundock/bulle/custom_profiles",
	} {
		if !strings.Contains(Usage(), example) {
			t.Fatalf("Usage() does not show install-profile example %q:\n%s", example, Usage())
		}
	}
	if !strings.Contains(Usage(), "--policy[=summary|json]") {
		t.Fatalf("Usage() does not show policy formats:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "summary by default") {
		t.Fatalf("Usage() does not explain default policy format:\n%s", Usage())
	}
	if !strings.Contains(Usage(), "macOS uses configured runtime roots") {
		t.Fatalf("Usage() does not explain macOS --add-libs behavior:\n%s", Usage())
	}
	for _, profile := range []string{"tool", "network", "offline", "macos-dns", "macos-certs", "keychain", "claude", "codex", "pi", "opencode"} {
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
