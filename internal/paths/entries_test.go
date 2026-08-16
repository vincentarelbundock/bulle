package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMarkers(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		path     string
		optional bool
		create   string
	}{
		{"~/.gitconfig", "~/.gitconfig", false, CreateNone},
		{"?~/.gitconfig", "~/.gitconfig", true, CreateNone},
		{"+~/.codex/", "~/.codex/", false, CreateDir},
		{"+~/.claude.json", "~/.claude.json", false, CreateFile},
		{"?+~/.x/", "~/.x/", true, CreateDir},
		{"+?~/.x", "~/.x", true, CreateFile},
	} {
		path, optional, create := ParseMarkers(tc.raw)
		if path != tc.path || optional != tc.optional || create != tc.create {
			t.Errorf("ParseMarkers(%q) = (%q, %v, %q), want (%q, %v, %q)", tc.raw, path, optional, create, tc.path, tc.optional, tc.create)
		}
	}
}

func TestCanonicalEntryKeyMergesMarkerSpellings(t *testing.T) {
	want := CanonicalEntryKey("/a/b")
	for _, raw := range []string{"?/a/b", "+/a/b/", "/a/b/", "/a//b"} {
		if got := CanonicalEntryKey(raw); got != want {
			t.Errorf("CanonicalEntryKey(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := CanonicalEntryKey("?which:codex"); got != "which:codex" {
		t.Errorf("CanonicalEntryKey resolver = %q", got)
	}
}

func TestResolveListOptionalMarkerSkipsMissing(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	out, traces, err := ResolveListTrace([]Input{{Path: "?" + missing, Source: SourceUser}}, Vars{"HOME": root})
	if err != nil {
		t.Fatalf("ResolveListTrace: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("out = %#v, want empty", out)
	}
	if len(traces) != 1 || traces[0].Outcome != "skipped (does not exist)" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestResolveListRequiredMissingFails(t *testing.T) {
	root := t.TempDir()
	_, _, err := ResolveListTrace([]Input{{Path: filepath.Join(root, "missing"), Source: SourceUser}}, Vars{"HOME": root})
	if err == nil {
		t.Fatalf("ResolveListTrace succeeded, want missing-path error")
	}
}

func TestResolveListCreatesMissingDirAndFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	file := filepath.Join(root, "nested", "state.json")
	out, traces, err := ResolveListTrace([]Input{
		{Path: "+" + dir + "/", Source: SourceUser},
		{Path: "+" + file, Source: SourceUser},
	}, Vars{"HOME": root})
	if err != nil {
		t.Fatalf("ResolveListTrace: %v", err)
	}
	// Each entry may resolve to an alias/real pair when the temporary
	// directory is reached through a symlink, as it is on macOS.
	for _, want := range []string{dir, file} {
		if !containsResolved(out, want) {
			t.Fatalf("out = %#v, missing %q", out, want)
		}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		t.Fatalf("file not created: %v", err)
	}
	if traces[0].Outcome != "created (dir)" || traces[1].Outcome != "created (file)" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestResolveListGlob(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"v1", "v2"} {
		if err := os.MkdirAll(filepath.Join(root, "versions", name, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, traces, err := ResolveListTrace([]Input{{Path: filepath.Join(root, "versions", "*", "bin"), Source: SourceUser}}, Vars{"HOME": root})
	if err != nil {
		t.Fatalf("ResolveListTrace: %v", err)
	}
	for _, name := range []string{"v1", "v2"} {
		want := filepath.Join(root, "versions", name, "bin")
		if !containsResolved(out, want) {
			t.Fatalf("out = %#v, missing %q", out, want)
		}
	}
	if traces[0].Outcome != "granted (2 matches)" {
		t.Fatalf("trace = %#v", traces[0])
	}
}

func TestResolveListGlobNoMatchesSkips(t *testing.T) {
	root := t.TempDir()
	out, traces, err := ResolveListTrace([]Input{{Path: filepath.Join(root, "nothing", "*"), Source: SourceUser}}, Vars{"HOME": root})
	if err != nil {
		t.Fatalf("ResolveListTrace: %v", err)
	}
	if len(out) != 0 || traces[0].Outcome != "skipped (no matches)" {
		t.Fatalf("out=%#v traces=%#v", out, traces)
	}
}

func TestResolveListRejectsDoubleStar(t *testing.T) {
	root := t.TempDir()
	_, _, err := ResolveListTrace([]Input{{Path: filepath.Join(root, "**", "bin"), Source: SourceUser}}, Vars{"HOME": root})
	if err == nil {
		t.Fatalf("ResolveListTrace succeeded, want ** rejection")
	}
}

func TestExpandFallbackSyntax(t *testing.T) {
	home := t.TempDir()
	vars := Vars{"HOME": home, "SET": "/explicit"}
	got, err := expand("${SET:-~/.fallback}", vars)
	if err != nil || got != "/explicit" {
		t.Fatalf("expand set = %q, %v", got, err)
	}
	got, err = expand("${UNSET:-~/.fallback}", vars)
	if err != nil || got != filepath.Join(home, ".fallback") {
		t.Fatalf("expand fallback = %q, %v", got, err)
	}
	if _, err := expand("$UNSET", vars); err == nil {
		t.Fatalf("expand succeeded, want unknown variable error")
	}
}

// containsResolved reports whether want appears in out under either spelling.
// A configured path reached through a symlink is granted as both the alias and
// its target, so an exact-length assertion says more about the machine's
// temporary directory than about the resolution being tested.
func containsResolved(out []string, want string) bool {
	candidates := []string{filepath.Clean(want)}
	if real, err := filepath.EvalSymlinks(want); err == nil {
		candidates = append(candidates, filepath.Clean(real))
	}
	for _, got := range out {
		for _, candidate := range candidates {
			if filepath.Clean(got) == candidate {
				return true
			}
		}
	}
	return false
}
