package backends

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFakeELF(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{0x7f, 'E', 'L', 'F', 0, 0}, mode); err != nil {
		t.Fatal(err)
	}
}

func TestScanTreesForELFSeedsFindsELFInExecTree(t *testing.T) {
	root := t.TempDir()
	writeFakeELF(t, filepath.Join(root, "bin", "exec", "R"), 0o755)
	writeFakeELF(t, filepath.Join(root, "lib", "libR.so"), 0o644)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := scanTreesForELFSeeds([]string{root}, nil)
	if result.truncated {
		t.Fatal("scan unexpectedly truncated")
	}
	if len(result.seeds) != 2 {
		t.Fatalf("seeds = %v, want the ELF executable and the shared library", result.seeds)
	}
}

func TestScanTreesForELFSeedsReadTreesOnlyUnderLibs(t *testing.T) {
	root := t.TempDir()
	writeFakeELF(t, filepath.Join(root, "pkg", "libs", "pkg.so"), 0o644)
	writeFakeELF(t, filepath.Join(root, "pkg", "data", "blob.so"), 0o644)

	result := scanTreesForELFSeeds(nil, []string{root})
	if len(result.seeds) != 1 || filepath.Base(result.seeds[0]) != "pkg.so" {
		t.Fatalf("seeds = %v, want only the libs/ shared object", result.seeds)
	}
}

func TestScanTreesForELFSeedsSkipsSystemRoots(t *testing.T) {
	result := scanTreesForELFSeeds([]string{"/usr", "/", "/bin"}, nil)
	if len(result.seeds) != 0 {
		t.Fatalf("seeds = %v, want none from system roots", result.seeds)
	}
}

func TestScanTreesForELFSeedsCollectsShebangInterpreters(t *testing.T) {
	root := t.TempDir()
	interp := filepath.Join(t.TempDir(), "bash")
	writeFakeELF(t, interp, 0o755)
	script := filepath.Join(root, "wrapper")
	if err := os.WriteFile(script, []byte("#!"+interp+"\nexec true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := scanTreesForELFSeeds([]string{root}, nil)
	if len(result.interpreters) != 1 || result.interpreters[0] != interp {
		t.Fatalf("interpreters = %v, want %q", result.interpreters, interp)
	}
}

func TestScriptStoreRefs(t *testing.T) {
	script := filepath.Join(t.TempDir(), "wrapper")
	body := "#!/bin/sh\n/nix/store/abc-gnused-4.9/bin/sed -e s/x/y/\nPATH=\"/nix/store/def-coreutils-9/bin:$PATH\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	refs := scriptStoreRefs(script)
	want := []string{"/nix/store/abc-gnused-4.9", "/nix/store/def-coreutils-9"}
	if len(refs) != 2 || refs[0] != want[0] || refs[1] != want[1] {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
}

func TestStoreItemRootFor(t *testing.T) {
	if root, ok := storeItemRootFor("/nix/store/abc-glibc-2.42/lib/libc.so.6"); !ok || root != "/nix/store/abc-glibc-2.42" {
		t.Fatalf("storeItemRootFor = %q, %v", root, ok)
	}
	if _, ok := storeItemRootFor("/usr/lib/libc.so.6"); ok {
		t.Fatal("expected no store item for /usr/lib")
	}
}

func TestScanTreesForELFSeedsIgnoresNonELFExecutables(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "wrapper")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := scanTreesForELFSeeds([]string{root}, nil)
	if len(result.seeds) != 0 {
		t.Fatalf("seeds = %v, want none for a shell wrapper", result.seeds)
	}
}
