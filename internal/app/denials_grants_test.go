package app

import (
	"strings"
	"testing"
)

func TestGrantSuggestionPathCollapsesStorePaths(t *testing.T) {
	cases := map[string]string{
		"/nix/store/abc123-openblas-0.3/lib/libblas.so.3": "/nix/store/abc123-openblas-0.3",
		"/nix/store/abc123-openblas-0.3":                  "/nix/store/abc123-openblas-0.3",
		"/opt/homebrew/Cellar/r/4.5.0/lib/R/bin/exec/R":   "/opt/homebrew/Cellar/r/4.5.0",
		"/home/user/.gitconfig":                           "/home/user/.gitconfig",
		"/usr/lib/libm.so.6":                              "/usr/lib/libm.so.6",
	}
	for path, want := range cases {
		if got := grantSuggestionPath(path); got != want {
			t.Errorf("grantSuggestionPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestDenialHintsCollapseIntoOneStoreGrant(t *testing.T) {
	lines := []string{
		`audit: type=1423 audit(1.0:30): domain=1 blockers=fs.read_file path="/nix/store/abc-lapack-3/lib/liblapack.so.3" dev="vda2" ino=1`,
		`audit: type=1423 audit(1.0:31): domain=1 blockers=fs.read_file path="/nix/store/abc-lapack-3/lib/libblas.so.3" dev="vda2" ino=2`,
	}
	hints := denialHints(parseLandlockDenials(lines, 0), "/home/user")
	if len(hints) != 1 {
		t.Fatalf("hints = %v, want the two denials collapsed into one store-root grant", hints)
	}
	if !strings.Contains(hints[0], "--ro /nix/store/abc-lapack-3") {
		t.Fatalf("hint = %q, want a /nix/store/abc-lapack-3 grant", hints[0])
	}
}
