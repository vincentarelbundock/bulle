//go:build linux || darwin

package trustedexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookPathRejectsWritableExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LookPath("git", dir); err == nil {
		t.Fatal("writable helper executable was trusted")
	}
}

func TestLookPathReturnsCanonicalImmutableExecutable(t *testing.T) {
	for _, candidate := range []string{"/usr/bin/true", "/bin/true"} {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		got, err := LookPath("true", filepath.Dir(candidate))
		if err != nil {
			t.Skipf("host executable is not immutable to this test identity: %v", err)
		}
		want, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("LookPath = %q, want %q", got, want)
		}
		return
	}
	t.Skip("host has no true executable in a standard directory")
}
