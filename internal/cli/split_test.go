package cli

import (
	"reflect"
	"testing"
)

func TestParseSeparatorStartsCommand(t *testing.T) {
	opts, err := Parse([]string{"bulle", "--", "git", "status"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Profile != "" || opts.ProjectPath != "." || !reflect.DeepEqual(opts.Command, []string{"git", "status"}) {
		t.Fatalf("profile=%q workspace=%q command=%#v", opts.Profile, opts.ProjectPath, opts.Command)
	}
}

func TestParseNeverInfersACommand(t *testing.T) {
	// Without --, positionals are the profile and the workspace; nothing is
	// ever read as a command, no matter what it looks like.
	opts, err := Parse([]string{"bulle", "git", "status"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts.Profile != "git" || opts.ProjectPath != "status" || opts.Command != nil {
		t.Fatalf("profile=%q workspace=%q command=%#v", opts.Profile, opts.ProjectPath, opts.Command)
	}
}

func TestParseFlagsAfterSeparatorBelongToCommand(t *testing.T) {
	opts, err := Parse([]string{"bulle", "tool", "--", "ls", "--color"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(opts.Command, []string{"ls", "--color"}) {
		t.Fatalf("command=%#v", opts.Command)
	}
}

func TestParseHelpAliasNotReadAsProfile(t *testing.T) {
	opts, err := Parse([]string{"bulle", "help"})
	if err != nil || !opts.Help {
		t.Fatalf("opts=%+v err=%v, want Help", opts, err)
	}
}
