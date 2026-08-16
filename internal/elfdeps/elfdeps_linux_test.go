//go:build linux

package elfdeps

import (
	"os"
	"testing"
)

// GetSystemLibraryDependencies resolves against the system library
// directories, so its fixture has to be a binary that actually links against
// them: a distribution's /usr/bin/true. On a machine that has no such binary
// (NixOS, where every library lives in the store under a hashed path) there is
// nothing here to test, and failing would report the layout of the machine.
func TestGetLibraryDependenciesForTrue(t *testing.T) {
	const binary = "/usr/bin/true"
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		t.Skipf("%s is not present on this machine", binary)
	}
	deps, err := GetSystemLibraryDependencies(binary)
	if err != nil {
		t.Fatalf("GetLibraryDependencies returned error: %v", err)
	}
	if len(deps) == 0 {
		t.Fatalf("deps is empty")
	}
}
