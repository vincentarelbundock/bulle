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
