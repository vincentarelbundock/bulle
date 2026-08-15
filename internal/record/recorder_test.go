package record

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsProbeArtifact(t *testing.T) {
	tmp := os.TempDir()
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}
	if !isProbeArtifact(filepath.Join(tmp, probeDirPrefix+"123", "denied")) {
		t.Error("probe temp file is not recognized")
	}
	if isProbeArtifact(filepath.Join(tmp, "something-else", "denied")) {
		t.Error("unrelated temp file recognized as a probe artifact")
	}
	// A directory of that name outside the temp root belongs to whoever made
	// it, and a denial there is the command's business.
	if isProbeArtifact(filepath.Join("/home/user", probeDirPrefix+"123", "denied")) {
		t.Error("path outside the temp root recognized as a probe artifact")
	}
}

func TestRecorderDeduplicatesGrantsButAccumulatesOrigins(t *testing.T) {
	// The same path denied to several processes is one Grant, but every
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
