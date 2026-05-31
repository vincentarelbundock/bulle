package paths

import "testing"

func TestCleanAbsoluteDeduplicatesAndSkipsRelativePaths(t *testing.T) {
	got := CleanAbsolute([]string{"/tmp/../tmp/bin", "relative", "", "/tmp/bin"})

	if len(got) != 1 || got[0] != "/tmp/bin" {
		t.Fatalf("CleanAbsolute = %#v, want only /tmp/bin", got)
	}
}

func TestIsWithinAnyRootAllowsRootAndChildrenOnly(t *testing.T) {
	roots := CleanAbsolute([]string{"/tmp/root"})

	for _, path := range []string{"/tmp/root", "/tmp/root/child"} {
		if !IsWithinAnyRoot(path, roots) {
			t.Fatalf("IsWithinAnyRoot(%q, %#v) = false, want true", path, roots)
		}
	}
	for _, path := range []string{"/tmp/root-other", "/tmp", "relative"} {
		if IsWithinAnyRoot(path, roots) {
			t.Fatalf("IsWithinAnyRoot(%q, %#v) = true, want false", path, roots)
		}
	}
}

func TestIsWithinAnyRootDefendsAgainstUncleanRoots(t *testing.T) {
	if IsWithinAnyRoot("/tmp/root/child", []string{"", "relative"}) {
		t.Fatalf("IsWithinAnyRoot matched empty or relative root")
	}
	if !IsWithinAnyRoot("/tmp/root/child", []string{"/tmp/other/../root"}) {
		t.Fatalf("IsWithinAnyRoot did not clean root")
	}
}
