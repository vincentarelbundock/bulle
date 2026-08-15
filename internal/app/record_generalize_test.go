package app

import (
	"fmt"
	"strings"
	"testing"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func testVars() bpaths.Vars {
	return bpaths.Vars{
		"HOME":            "/home/user",
		"WORKSPACE":       "/home/user/repos/proj",
		"CACHE":           "/home/user/.cache",
		"XDG_CACHE_HOME":  "/home/user/.cache",
		"CONFIG":          "/home/user/.config",
		"XDG_CONFIG_HOME": "/home/user/.config",
		"TMP":             "/tmp",
		"TMPDIR":          "/tmp",
		"GOMODCACHE":      "/home/user/go/pkg/mod",
	}
}

func testResolvers() []policy.ResolverListing {
	return []policy.ResolverListing{
		{Entry: "go:modcache", Outcome: "ok", Paths: []string{"/home/user/go/pkg/mod"}},
		{Entry: "r:home", Outcome: "ok", Paths: []string{"/usr/lib/R"}},
		{Entry: "uv:cache", Outcome: "unavailable (uv not found)"},
		{Entry: "npm:cache", Outcome: "ok", Paths: []string{"/"}},
	}
}

func noLookPath(string) (string, error) { return "", fmt.Errorf("not found") }

func newTestGeneralizer(lookPath func(string) (string, error)) *generalizer {
	return newGeneralizer(testVars(), testResolvers(), lookPath)
}

func TestGeneralizeRewritesPathsToPortableEntries(t *testing.T) {
	g := newTestGeneralizer(noLookPath)
	cases := []struct {
		name  string
		gr    grant
		want  string
		list  string
		blank bool // Comment must be empty
	}{
		{
			name: "home-relative path uses the most specific variable",
			gr:   grant{Flag: "--ro", Path: "/home/user/.config/pandoc/defaults.yaml"},
			want: "$CONFIG/pandoc/defaults.yaml",
			list: "ro", blank: true,
		},
		{
			name: "plain home path falls back to $HOME",
			gr:   grant{Flag: "--rw", Path: "/home/user/.gitconfig"},
			want: "$HOME/.gitconfig",
			list: "rw", blank: true,
		},
		{
			name: "resolver directory beats the variable that also matches",
			gr:   grant{Flag: "--rw", Path: "/home/user/go/pkg/mod/github.com/x/y@v1/go.mod"},
			want: "go:modcache",
			list: "rw",
		},
		{
			name: "exact resolver path needs no explanation",
			gr:   grant{Flag: "--rox", Path: "/usr/lib/R"},
			want: "r:home",
			list: "rox", blank: true,
		},
		{
			name: "unmatched path stays literal",
			gr:   grant{Flag: "--ro", Path: "/etc/ssl/certs/ca-bundle.crt"},
			want: "/etc/ssl/certs/ca-bundle.crt",
			list: "ro", blank: true,
		},
		{
			name: "sibling directory does not match a variable prefix",
			gr:   grant{Flag: "--ro", Path: "/home/user-backup/notes"},
			want: "/home/user-backup/notes",
			list: "ro", blank: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := g.generalize(tc.gr)
			if got.Entry != tc.want {
				t.Errorf("entry = %q, want %q", got.Entry, tc.want)
			}
			if got.List != tc.list {
				t.Errorf("list = %q, want %q", got.List, tc.list)
			}
			if tc.blank && got.Comment != "" {
				t.Errorf("comment = %q, want empty", got.Comment)
			}
			if got.Denied != tc.gr.Path {
				t.Errorf("denied = %q, want %q", got.Denied, tc.gr.Path)
			}
		})
	}
}

func TestGeneralizeNeverEmitsABareVariableRoot(t *testing.T) {
	g := newTestGeneralizer(noLookPath)
	// A denial on $HOME or $CACHE itself must not become a grant on the whole
	// tree; it stays literal so a human has to decide.
	for _, path := range []string{"/home/user", "/home/user/.cache"} {
		got := g.generalize(grant{Flag: "--ro", Path: path})
		if got.Entry != path {
			t.Errorf("generalize(%q) = %q, want the literal path", path, got.Entry)
		}
	}
}

func TestGeneralizeIgnoresUnusableSubstitutions(t *testing.T) {
	g := newTestGeneralizer(noLookPath)
	// npm:cache resolved to "/" and must never match; uv:cache was
	// unavailable and must not appear at all.
	got := g.generalize(grant{Flag: "--ro", Path: "/opt/thing/file"})
	if got.Entry != "/opt/thing/file" {
		t.Fatalf("entry = %q, want the literal path", got.Entry)
	}
	for _, sub := range g.subs {
		if sub.Entry == "npm:cache" || sub.Entry == "uv:cache" {
			t.Errorf("substitution table contains %q", sub.Entry)
		}
	}
}

func TestGeneralizeRecognizesCommandsOnPath(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "pandoc" {
			return "/usr/local/bin/pandoc", nil
		}
		return "", fmt.Errorf("not found")
	}
	g := newTestGeneralizer(lookPath)

	got := g.generalize(grant{Flag: "--rox", Path: "/usr/local/bin/pandoc"})
	if got.Entry != "which:pandoc" {
		t.Errorf("entry = %q, want which:pandoc", got.Entry)
	}

	// A read of the same file is not an executable grant, so the which:
	// spelling does not apply.
	if got := g.generalize(grant{Flag: "--ro", Path: "/usr/local/bin/pandoc"}); got.Entry != "/usr/local/bin/pandoc" {
		t.Errorf("read entry = %q, want the literal path", got.Entry)
	}

	// A different binary of the same name is not the one PATH resolves.
	if got := g.generalize(grant{Flag: "--rox", Path: "/opt/other/bin/pandoc"}); got.Entry != "/opt/other/bin/pandoc" {
		t.Errorf("entry = %q, want the literal path", got.Entry)
	}
}

func TestMergeEntriesKeepsTheWeakestCoveringAccess(t *testing.T) {
	merged := mergeEntries([]recordedEntry{
		{List: "ro", Entry: "$HOME/.local/bin/tool"},
		{List: "rox", Entry: "$HOME/.local/bin/tool"},
		{List: "rw", Entry: "$HOME/.local/bin/tool"},
		{List: "ro", Entry: "$CONFIG/a"},
		{Entry: ""},
	})
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want 2 entries", merged)
	}
	byEntry := map[string]string{}
	for _, e := range merged {
		byEntry[e.Entry] = e.List
	}
	if byEntry["$HOME/.local/bin/tool"] != "rwx" {
		t.Errorf("tool list = %q, want rwx", byEntry["$HOME/.local/bin/tool"])
	}
	if byEntry["$CONFIG/a"] != "ro" {
		t.Errorf("config list = %q, want ro", byEntry["$CONFIG/a"])
	}
}

func TestPromoteDirectoriesCollapsesSiblingClusters(t *testing.T) {
	entries := promoteDirectories([]recordedEntry{
		{List: "ro", Entry: "$CACHE/tool/a"},
		{List: "ro", Entry: "$CACHE/tool/b"},
		{List: "ro", Entry: "$CACHE/tool/c"},
		{List: "ro", Entry: "$CACHE/other/only"},
	})
	want := map[string]bool{"$CACHE/tool/": true, "$CACHE/other/only": true}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2", entries)
	}
	for _, e := range entries {
		if !want[e.Entry] {
			t.Errorf("unexpected entry %q", e.Entry)
		}
	}
}

func TestPromoteDirectoriesRespectsTheFloor(t *testing.T) {
	// Three files directly inside $HOME must not promote to a grant on $HOME.
	entries := promoteDirectories([]recordedEntry{
		{List: "ro", Entry: "$HOME/.a"},
		{List: "ro", Entry: "$HOME/.b"},
		{List: "ro", Entry: "$HOME/.c"},
	})
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want the three originals kept", entries)
	}

	// Same for a cluster directly inside a top-level system directory.
	entries = promoteDirectories([]recordedEntry{
		{List: "ro", Entry: "/etc/a"},
		{List: "ro", Entry: "/etc/b"},
		{List: "ro", Entry: "/etc/c"},
	})
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want the three originals kept", entries)
	}
}

func TestPromoteDirectoriesLeavesDistinctListsAlone(t *testing.T) {
	// Three siblings, but not all with the same access: promoting them
	// together would hand write access to the readable ones.
	entries := promoteDirectories([]recordedEntry{
		{List: "ro", Entry: "$CACHE/tool/a"},
		{List: "ro", Entry: "$CACHE/tool/b"},
		{List: "rw", Entry: "$CACHE/tool/c"},
	})
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want no promotion across access levels", entries)
	}
}

func TestPromoteDirectoriesDoesNotWidenDirectoryGrants(t *testing.T) {
	entries := promoteDirectories([]recordedEntry{
		{List: "ro", Entry: "$CACHE/tool/a/"},
		{List: "ro", Entry: "$CACHE/tool/b/"},
		{List: "ro", Entry: "$CACHE/tool/c/"},
		{List: "ro", Entry: "go:modcache"},
	})
	if len(entries) != 4 {
		t.Fatalf("entries = %+v, want directory and resolver entries untouched", entries)
	}
}

func TestGeneralizeExplainsAWholeProcGrant(t *testing.T) {
	g := newTestGeneralizer(noLookPath)
	got := g.generalize(grant{Flag: "--ro", Path: "/proc"})
	if got.Entry != "/proc" {
		t.Fatalf("entry = %q, want /proc", got.Entry)
	}
	// The widening is unavoidable but must not be silent.
	if !strings.Contains(got.Comment, "same-uid") {
		t.Errorf("comment does not state the tradeoff: %q", got.Comment)
	}
}
