//go:build linux

package elfdeps

import "testing"

func TestGetLibraryDependenciesForTrue(t *testing.T) {
	deps, err := GetSystemLibraryDependencies("/usr/bin/true")
	if err != nil {
		t.Fatalf("GetLibraryDependencies returned error: %v", err)
	}
	if len(deps) == 0 {
		t.Fatalf("deps is empty")
	}
}
