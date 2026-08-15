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

func TestGrantForDenialReportsFlagAndCollapsedPath(t *testing.T) {
	cases := []struct {
		name     string
		denial   landlockDenial
		wantOK   bool
		wantFlag string
		wantPath string
	}{
		{
			name:     "read",
			denial:   landlockDenial{Blockers: []string{"fs.read_file"}, Path: "/home/user/.gitconfig"},
			wantOK:   true,
			wantFlag: "--ro",
			wantPath: "/home/user/.gitconfig",
		},
		{
			name:     "strongest access wins",
			denial:   landlockDenial{Blockers: []string{"fs.read_file", "fs.execute", "fs.write_file"}, Path: "/tmp/build/a.out"},
			wantOK:   true,
			wantFlag: "--rwx",
			wantPath: "/tmp/build/a.out",
		},
		{
			name:     "store path collapses to the package root",
			denial:   landlockDenial{Blockers: []string{"fs.execute"}, Path: "/nix/store/abc-r-4.5/lib/R/bin/exec/R"},
			wantOK:   true,
			wantFlag: "--rox",
			wantPath: "/nix/store/abc-r-4.5",
		},
		{
			name:   "network denial names no grantable path",
			denial: landlockDenial{Blockers: []string{"net.connect_tcp"}},
		},
		{
			name:   "pathless file denial",
			denial: landlockDenial{Blockers: []string{"fs.read_file"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := grantForDenial(tc.denial)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Flag != tc.wantFlag || got.Path != tc.wantPath {
				t.Errorf("grant = %+v, want {Flag:%s Path:%s}", got, tc.wantFlag, tc.wantPath)
			}
		})
	}
}

func TestGrantForSeatbeltDenialReportsFlagAndCollapsedPath(t *testing.T) {
	cases := []struct {
		name     string
		denial   seatbeltDenial
		wantOK   bool
		wantFlag string
		wantPath string
	}{
		{
			name:     "read",
			denial:   seatbeltDenial{Operation: "file-read-data", Path: "/Users/v/.gitconfig"},
			wantOK:   true,
			wantFlag: "--ro",
			wantPath: "/Users/v/.gitconfig",
		},
		{
			name:     "exec",
			denial:   seatbeltDenial{Operation: "process-exec", Path: "/opt/homebrew/Cellar/r/4.5.0/lib/R/bin/exec/R"},
			wantOK:   true,
			wantFlag: "--rox",
			wantPath: "/opt/homebrew/Cellar/r/4.5.0",
		},
		{
			name:   "network operation names no grantable path",
			denial: seatbeltDenial{Operation: "network-outbound", Path: "*:443"},
		},
		{
			name:   "redacted path",
			denial: seatbeltDenial{Operation: "file-read-data", Path: "<private>"},
		},
		{
			name:   "relative target",
			denial: seatbeltDenial{Operation: "file-read-data", Path: "some-relative-thing"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := grantForSeatbeltDenial(tc.denial)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Flag != tc.wantFlag || got.Path != tc.wantPath {
				t.Errorf("grant = %+v, want {Flag:%s Path:%s}", got, tc.wantFlag, tc.wantPath)
			}
		})
	}
}

func TestGrantSuggestionPathCollapsesPerProcessProcEntries(t *testing.T) {
	cases := map[string]string{
		// The pid differs for every process, so the denied path is never the
		// path to grant.
		"/proc/1234/cgroup":       "/proc",
		"/proc/1/status":          "/proc",
		"/proc/1234":              "/proc",
		"/proc/self/maps":         "/proc/self/maps",
		"/proc/stat":              "/proc/stat",
		"/proc/sys/vm/swappiness": "/proc/sys/vm/swappiness",
		"/proc":                   "/proc",
	}
	for path, want := range cases {
		if got := grantSuggestionPath(path); got != want {
			t.Errorf("grantSuggestionPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestDenialHintsCollapsePerProcessProcEntries(t *testing.T) {
	// Without collapsing, a tool that reads /proc/<pid>/... in each of its
	// children produces one hint per child and a recording never converges.
	lines := []string{
		`audit: type=1423 audit(1.0:1): domain=1 blockers=fs.read_file path="/proc/111/cgroup" dev="proc" ino=1`,
		`audit: type=1423 audit(1.0:2): domain=1 blockers=fs.read_file path="/proc/222/cgroup" dev="proc" ino=2`,
		`audit: type=1423 audit(1.0:3): domain=1 blockers=fs.read_file path="/proc/333/status" dev="proc" ino=3`,
	}
	hints := denialHints(parseLandlockDenials(lines, 0), "/home/user")
	if len(hints) != 1 {
		t.Fatalf("hints = %v, want one collapsed /proc grant", hints)
	}
	if !strings.Contains(hints[0], "--ro /proc") {
		t.Fatalf("hint = %q, want a /proc grant", hints[0])
	}
}
