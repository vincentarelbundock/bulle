package record

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestLearnTargetProfile(t *testing.T) {
	global := config.DefaultConfig()
	name, create := learnTargetProfile(cli.Options{Profile: "claude,offline"}, global)
	if name != "claude" || create {
		t.Fatalf("got %q create=%v, want claude (existing)", name, create)
	}
	name, create = learnTargetProfile(cli.Options{Command: []string{"/usr/bin/mytool", "arg"}}, global)
	if name != "mytool" || !create {
		t.Fatalf("got %q create=%v, want mytool (new)", name, create)
	}
	name, _ = learnTargetProfile(cli.Options{}, global)
	if name != "" {
		t.Fatalf("no profile and no command should have no target, got %q", name)
	}
}

func TestReportLearnedGrantsShowsGeneralizedEntriesAndTheFileToEdit(t *testing.T) {
	root := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory to generalize against")
	}
	rec := NewRecorder()
	// Three denials under one directory in the user's home: the report names the
	// directory, spelled with the variable, and the literal paths the kernel
	// reported never reach the user.
	rec.grants = append(rec.grants,
		Grant{Flag: "--ro", Path: filepath.Join(home, ".cache", "toolcache", "a.json")},
		Grant{Flag: "--ro", Path: filepath.Join(home, ".cache", "toolcache", "b.json")},
		Grant{Flag: "--ro", Path: filepath.Join(home, ".cache", "toolcache", "c.json")},
	)
	var out bytes.Buffer
	ReportLearnedGrants(cli.Options{Profile: "claude", Flags: cli.Flags{Config: root}}, config.DefaultConfig(), rec, nil, &out)

	got := out.String()
	for _, want := range []string{"?$CACHE/toolcache/", "claude", "profiles/claude.toml", "denied accesses"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report = %q, want it to contain %q", got, want)
		}
	}
	for _, unwanted := range []string{"a.json", "b.json", "c.json", home, "[s]ave", "[w]rite"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("report = %q, want it not to contain %q", got, unwanted)
		}
	}
}
func TestReportLearnedGrantsSaysNothingWithoutGrants(t *testing.T) {
	var out bytes.Buffer
	ReportLearnedGrants(cli.Options{Profile: "claude"}, config.DefaultConfig(), NewRecorder(), nil, &out)
	if out.Len() != 0 {
		t.Fatalf("report with no grants = %q, want nothing", out.String())
	}
}
