package record

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
		gr   Grant
		want bool
	}{
		{"read inside a read-only root", Grant{Flag: "--ro", Path: "/etc/ssl/certs/ca.crt"}, true},
		{"read of the root itself", Grant{Flag: "--ro", Path: "/etc/ssl"}, true},
		{"read inside an exec root", Grant{Flag: "--ro", Path: "/usr/bin/pandoc"}, true},
		{"exec inside a read-only root", Grant{Flag: "--rox", Path: "/etc/ssl/x"}, false},
		{"exec inside an exec root", Grant{Flag: "--rox", Path: "/usr/bin/pandoc"}, true},
		{"write inside a read-only root", Grant{Flag: "--rw", Path: "/etc/ssl/x"}, false},
		{"write inside a writable root", Grant{Flag: "--rw", Path: "/home/user/.cache/tool/db"}, true},
		{"exec inside a writable root", Grant{Flag: "--rox", Path: "/home/user/.cache/tool/x"}, false},
		{"exec+write inside a rwx root", Grant{Flag: "--rwx", Path: "/tmp/build/a.out"}, true},
		{"unrelated path", Grant{Flag: "--ro", Path: "/opt/thing"}, false},
		{"sibling of a granted root", Grant{Flag: "--ro", Path: "/etc/ssl-backup/x"}, false},
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
	// symlinked directory records a duplicate of a Grant that already exists.
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
	if !coveredByPolicy(Grant{Flag: "--ro", Path: filepath.Join(link, "sub")}, p) {
		t.Error("Grant through the link is not covered by a Grant on the link")
	}

	p = policy.Policy{ReadOnly: []string{real}}
	if !coveredByPolicy(Grant{Flag: "--ro", Path: filepath.Join(link, "sub")}, p) {
		t.Error("Grant through the link is not covered by a Grant on the target")
	}
}

func TestFilterCoveredGrantsKeepsOnlyWhatIsMissing(t *testing.T) {
	p := policy.Policy{ReadOnly: []string{"/etc/ssl"}}
	grants := []ObservedGrant{
		{Grant: Grant{Flag: "--ro", Path: "/etc/ssl/certs/ca.crt"}},
		{Grant: Grant{Flag: "--ro", Path: "/home/user/.gitconfig"}},
		{Grant: Grant{Flag: "--rox", Path: "/etc/ssl/weird"}, Origin: "curl"},
	}
	kept := filterCoveredGrants(grants, p)
	if len(kept) != 2 {
		t.Fatalf("kept = %+v, want the two uncovered grants", kept)
	}
	if kept[0].Grant.Path != "/home/user/.gitconfig" || kept[1].Grant.Path != "/etc/ssl/weird" {
		t.Errorf("kept = %+v, want original order preserved", kept)
	}
	if kept[1].Origin != "curl" {
		t.Errorf("origin = %q, want it carried through the filter", kept[1].Origin)
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
	want := []Grant{
		{Flag: "--ro", Path: "/etc/a"},
		{Flag: "--rox", Path: "/etc/a"},
		{Flag: "--ro", Path: "/etc/b"},
	}
	if len(grants) != len(want) {
		t.Fatalf("grants = %+v, want %+v", grants, want)
	}
	for i := range want {
		if grants[i].Grant != want[i] {
			t.Errorf("grants[%d] = %+v, want %+v", i, grants[i].Grant, want[i])
		}
		// Landlock records name a security domain, not a process.
		if grants[i].Origin != "" {
			t.Errorf("grants[%d] origin = %q, want empty on Linux", i, grants[i].Origin)
		}
	}
}

func TestGrantsForSeatbeltDenialsKeepTheProcessName(t *testing.T) {
	// macOS reports violations from every sandboxed process on the machine, so
	// who was denied is the reviewer's only way to spot an unrelated entry.
	grants := grantsForSeatbeltDenials([]seatbeltDenial{
		{Process: "mytool", Operation: "file-read-data", Path: "/etc/a"},
		{Process: "mdworker", Operation: "file-read-data", Path: "/etc/b"},
		{Process: "mytool", Operation: "network-outbound", Path: "*:443"},
	})
	if len(grants) != 2 {
		t.Fatalf("grants = %+v, want the two grantable paths", grants)
	}
	if grants[0].Origin != "mytool" || grants[1].Origin != "mdworker" {
		t.Errorf("origins = %q, %q, want mytool, mdworker", grants[0].Origin, grants[1].Origin)
	}
}
