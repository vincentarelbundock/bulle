package app

import (
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

func TestSaveLearnedGrantsCreatesAndMerges(t *testing.T) {
	root := t.TempDir()
	opts := cli.Options{Flags: cli.Flags{Config: root}, Command: []string{"mytool"}}
	global := config.DefaultConfig()

	path, err := saveLearnedGrants(opts, global, "mytool", true, []grant{
		{Flag: "--ro", Path: "/etc/mytool.conf"},
	})
	if err != nil {
		t.Fatalf("saveLearnedGrants: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{learnedMarker, `inherits = ["default"]`, `default_app = "mytool"`, `"?/etc/mytool.conf"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved profile missing %q:\n%s", want, text)
		}
	}

	// A second save unions into the same file without losing anything.
	if _, err := saveLearnedGrants(opts, global, "mytool", true, []grant{
		{Flag: "--ro", Path: "/etc/mytool.conf"},
		{Flag: "--rw", Path: "/var/lib/mytool"},
	}); err != nil {
		t.Fatalf("second save: %v", err)
	}
	data, _ = os.ReadFile(path)
	text = string(data)
	for _, want := range []string{`"?/etc/mytool.conf"`, `"?/var/lib/mytool"`, `default_app = "mytool"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged profile missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "/etc/mytool.conf") != 1 {
		t.Fatalf("duplicate entries after merge:\n%s", text)
	}

	// The saved file must load as a profile named after the file.
	loaded, err := config.LoadProfileDirectory(filepath.Join(root, "profiles"))
	if err != nil {
		t.Fatalf("saved profile does not load: %v", err)
	}
	if _, ok := loaded.Profiles["mytool"]; !ok {
		t.Fatalf("profile mytool missing from loaded config: %#v", loaded.Profiles)
	}
}

func TestSaveLearnedGrantsRefusesForeignFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claude.toml"), []byte("ro = [\"/etc\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := cli.Options{Flags: cli.Flags{Config: root}, Profile: "claude"}
	_, err := saveLearnedGrants(opts, config.DefaultConfig(), "claude", false, []grant{{Flag: "--ro", Path: "/etc/x"}})
	if err == nil || !strings.Contains(err.Error(), "not written by bulle") {
		t.Fatalf("err = %v, want refusal to rewrite a hand-written file", err)
	}
}
