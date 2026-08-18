//go:build linux

package record

import "testing"

func TestDenialsForMarkerDomainRejectsConcurrentSandboxes(t *testing.T) {
	marker := "/tmp/.bulle-landlock-audit-unique"
	all := []landlockDenial{
		{Domain: "other", Path: "/home/user/.ssh/id_ed25519", Blockers: []string{"fs.read_file"}},
		{Domain: "ours", Path: marker, Blockers: []string{"fs.read_file"}},
		{Domain: "ours", Path: "/home/user/.gitconfig", Blockers: []string{"fs.read_file"}},
		{Domain: "other", Path: "/etc/shadow", Blockers: []string{"fs.read_file"}},
	}
	got := denialsForMarkerDomain(all, marker)
	if len(got) != 1 || got[0].Domain != "ours" || got[0].Path != "/home/user/.gitconfig" {
		t.Fatalf("scoped denials = %+v, want only this run's non-marker denial", got)
	}
}

func TestDenialsForMarkerDomainFailsClosedWithoutMarker(t *testing.T) {
	got := denialsForMarkerDomain([]landlockDenial{{Domain: "other", Path: "/etc/shadow"}}, "/missing-marker")
	if len(got) != 0 {
		t.Fatalf("unscoped denials = %+v, want none", got)
	}
}
