package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseInfersCommandWithoutSeparator(t *testing.T) {
	dir := t.TempDir()

	opts, err := Parse([]string{"bulle", dir, "git", "status"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.ProjectPath != dir || !reflect.DeepEqual(opts.Command, []string{"git", "status"}) {
		t.Fatalf("workspace=%q command=%#v", opts.ProjectPath, opts.Command)
	}
	if len(opts.Notes) != 1 {
		t.Fatalf("Notes = %#v, want inference note", opts.Notes)
	}

	opts, err = Parse([]string{"bulle", "git", "status"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.ProjectPath != "." || !reflect.DeepEqual(opts.Command, []string{"git", "status"}) {
		t.Fatalf("workspace=%q command=%#v", opts.ProjectPath, opts.Command)
	}
}

func TestParseInferenceSkipsFlagValues(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--profile", "tool", "--env", "PATH", "git", "status"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Profile != "tool" || !reflect.DeepEqual(opts.Command, []string{"git", "status"}) {
		t.Fatalf("profile=%q command=%#v", opts.Profile, opts.Command)
	}
}

func TestParseAmbiguityResolvesTowardWorkspace(t *testing.T) {
	parent := t.TempDir()
	sub := filepath.Join(parent, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// First positional is a directory: workspace. Second, even though it is
	// also a directory, starts the command.
	opts, err := Parse([]string{"bulle", parent, sub})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.ProjectPath != parent || !reflect.DeepEqual(opts.Command, []string{sub}) {
		t.Fatalf("workspace=%q command=%#v", opts.ProjectPath, opts.Command)
	}
}

func TestParseExplicitSeparatorStillWins(t *testing.T) {
	dir := t.TempDir()
	opts, err := Parse([]string{"bulle", "--", dir})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(opts.Command, []string{dir}) || len(opts.Notes) != 0 {
		t.Fatalf("command=%#v notes=%#v", opts.Command, opts.Notes)
	}
}

func TestParseHelpAliasNotInferredAsCommand(t *testing.T) {
	opts, err := Parse([]string{"bulle", "help"})
	if err != nil || !opts.Help {
		t.Fatalf("opts=%+v err=%v, want Help", opts, err)
	}
}
