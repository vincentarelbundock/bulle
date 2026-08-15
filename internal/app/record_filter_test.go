package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestCoveredByPolicyChecksEachAccessAgainstItsLists(t *testing.T) {
	p := policy.Policy{
		ReadOnly:      []string{"/etc/ssl"},
		ReadOnlyExec:  []string{"/usr/bin"},
		ReadWrite:     []string{"/home/user/.cache/tool"},
		ReadWriteExec: []string{"/tmp/build"},
	}
	cases := []struct {
		name string
		gr   grant
		want bool
	}{
		{"read inside a read-only root", grant{Flag: "--ro", Path: "/etc/ssl/certs/ca.crt"}, true},
		{"read of the root itself", grant{Flag: "--ro", Path: "/etc/ssl"}, true},
		{"read inside an exec root", grant{Flag: "--ro", Path: "/usr/bin/pandoc"}, true},
		{"exec inside a read-only root", grant{Flag: "--rox", Path: "/etc/ssl/x"}, false},
		{"exec inside an exec root", grant{Flag: "--rox", Path: "/usr/bin/pandoc"}, true},
		{"write inside a read-only root", grant{Flag: "--rw", Path: "/etc/ssl/x"}, false},
		{"write inside a writable root", grant{Flag: "--rw", Path: "/home/user/.cache/tool/db"}, true},
		{"exec inside a writable root", grant{Flag: "--rox", Path: "/home/user/.cache/tool/x"}, false},
		{"exec+write inside a rwx root", grant{Flag: "--rwx", Path: "/tmp/build/a.out"}, true},
		{"unrelated path", grant{Flag: "--ro", Path: "/opt/thing"}, false},
		{"sibling of a granted root", grant{Flag: "--ro", Path: "/etc/ssl-backup/x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := coveredByPolicy(tc.gr, p); got != tc.want {
				t.Errorf("coveredByPolicy(%+v) = %v, want %v", tc.gr, got, tc.want)
			}
		})
	}
}

func TestCoveredByPolicyFollowsSymlinksToTheGrantedPath(t *testing.T) {
	// The kernel reports the path it resolved, while a profile commonly grants
	// the link. Both spellings must count as covered, or every run through a
	// symlinked directory records a duplicate of a grant that already exists.
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	p := policy.Policy{ReadOnly: []string{link}}
	if !coveredByPolicy(grant{Flag: "--ro", Path: filepath.Join(link, "sub")}, p) {
		t.Error("grant through the link is not covered by a grant on the link")
	}

	p = policy.Policy{ReadOnly: []string{real}}
	if !coveredByPolicy(grant{Flag: "--ro", Path: filepath.Join(link, "sub")}, p) {
		t.Error("grant through the link is not covered by a grant on the target")
	}
}

func TestFilterCoveredGrantsKeepsOnlyWhatIsMissing(t *testing.T) {
	p := policy.Policy{ReadOnly: []string{"/etc/ssl"}}
	grants := []grant{
		{Flag: "--ro", Path: "/etc/ssl/certs/ca.crt"},
		{Flag: "--ro", Path: "/home/user/.gitconfig"},
		{Flag: "--rox", Path: "/etc/ssl/weird"},
	}
	kept := filterCoveredGrants(grants, p)
	if len(kept) != 2 {
		t.Fatalf("kept = %+v, want the two uncovered grants", kept)
	}
	if kept[0].Path != "/home/user/.gitconfig" || kept[1].Path != "/etc/ssl/weird" {
		t.Errorf("kept = %+v, want original order preserved", kept)
	}
}

func TestGrantsForDenialsDeduplicatesAndSkipsUngrantable(t *testing.T) {
	denials := []landlockDenial{
		{Blockers: []string{"fs.read_file"}, Path: "/etc/a"},
		{Blockers: []string{"fs.read_file"}, Path: "/etc/a"},
		{Blockers: []string{"net.connect_tcp"}},
		{Blockers: []string{"fs.execute"}, Path: "/etc/a"},
		{Blockers: []string{"fs.read_file"}, Path: "/etc/b"},
	}
	grants := grantsForDenials(denials)
	want := []grant{
		{Flag: "--ro", Path: "/etc/a"},
		{Flag: "--rox", Path: "/etc/a"},
		{Flag: "--ro", Path: "/etc/b"},
	}
	if len(grants) != len(want) {
		t.Fatalf("grants = %+v, want %+v", grants, want)
	}
	for i := range want {
		if grants[i] != want[i] {
			t.Errorf("grants[%d] = %+v, want %+v", i, grants[i], want[i])
		}
	}
}
