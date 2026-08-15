package record

import (
	"testing"
)

func TestRecorderDeduplicatesGrantsButAccumulatesOrigins(t *testing.T) {
	// The same path denied to several processes is one grant, but every
	// process that hit it is worth showing.
	rec := NewRecorder()
	gr := Grant{Flag: "--ro", Path: "/etc/a"}
	rec.noteOrigin(gr, "mytool")
	rec.noteOrigin(gr, "helper")
	rec.noteOrigin(gr, "mytool")
	rec.noteOrigin(gr, "")
	if got := rec.origins[gr]; len(got) != 2 || got[0] != "mytool" || got[1] != "helper" {
		t.Errorf("origins = %v, want [mytool helper]", got)
	}
}

func TestRecorderTracksSavedGrants(t *testing.T) {
	rec := NewRecorder()
	a := Grant{Flag: "--ro", Path: "/etc/a"}
	b := Grant{Flag: "--rw", Path: "/etc/b"}
	rec.grants = append(rec.grants, a)
	rec.MarkSaved()
	rec.grants = append(rec.grants, b)
	if got := rec.Unsaved(); len(got) != 1 || got[0] != b {
		t.Errorf("Unsaved = %v, want just %v", got, b)
	}
	rec.MarkSaved()
	if got := rec.Unsaved(); len(got) != 0 {
		t.Errorf("Unsaved after save = %v, want none", got)
	}
}
