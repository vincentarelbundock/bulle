package elfdeps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSingleSonameRejectsNonELFFile(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("not an ELF dependency"), 0o600); err != nil {
		t.Fatal(err)
	}
	var ldmap map[string]string

	got := resolveSingleSoname("secret.txt", []string{dir}, nil, &ldmap)

	if got != "" {
		t.Fatalf("resolveSingleSoname returned %q for non-ELF file, want empty", got)
	}
}

func TestResolveSingleSonameRejectsPathLikeSoname(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(nested, "secret.txt")
	if err := os.WriteFile(secret, []byte("not an ELF dependency"), 0o600); err != nil {
		t.Fatal(err)
	}
	var ldmap map[string]string

	got := resolveSingleSoname("nested/secret.txt", []string{dir}, nil, &ldmap)

	if got != "" {
		t.Fatalf("resolveSingleSoname returned %q for path-like soname, want empty", got)
	}
}

func TestGetLdmapDoesNotUseAmbientPATH(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	fake := filepath.Join(dir, "ldconfig")
	script := "#!/bin/sh\n" +
		"printf ran > " + marker + "\n" +
		"printf 'libfake.so (libc6) => /tmp/libfake.so\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_ = getLdmap()

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("getLdmap executed ldconfig from ambient PATH")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestFilterRpathsKeepsOnlyTrustedRoots(t *testing.T) {
	trusted := t.TempDir()
	untrusted := t.TempDir()
	trustedLib := filepath.Join(trusted, "lib")
	untrustedLib := filepath.Join(untrusted, "lib")
	if err := os.Mkdir(trustedLib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(untrustedLib, 0o755); err != nil {
		t.Fatal(err)
	}

	got := filterRpathsByRoots([]string{trustedLib, untrustedLib}, []string{trusted})
	wantTrusted, err := filepath.EvalSymlinks(trustedLib)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != wantTrusted {
		t.Fatalf("filterRpathsByRoots = %#v, want only trusted rpath", got)
	}
}

func TestFilterRpathsAllowsSymlinkedTrustedRoot(t *testing.T) {
	realTrusted := t.TempDir()
	aliasRoot := filepath.Join(t.TempDir(), "trusted")
	realLib := filepath.Join(realTrusted, "lib")
	if err := os.Mkdir(realLib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTrusted, aliasRoot); err != nil {
		t.Fatal(err)
	}
	aliasLib := filepath.Join(aliasRoot, "lib")

	got := filterRpathsByRoots([]string{aliasLib}, []string{aliasRoot})
	wantLib, err := filepath.EvalSymlinks(aliasLib)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != wantLib {
		t.Fatalf("filterRpathsByRoots = %#v, want real path %q under trusted root alias", got, wantLib)
	}
}

func TestFilterRpathsRejectsSymlinkEscape(t *testing.T) {
	trusted := t.TempDir()
	untrusted := t.TempDir()
	target := filepath.Join(untrusted, "lib")
	link := filepath.Join(trusted, "lib")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got := filterRpathsByRoots([]string{link}, []string{trusted})

	if len(got) != 0 {
		t.Fatalf("filterRpathsByRoots = %#v, want symlink escape rejected", got)
	}
}

func TestRuntimePathAllowedRejectsPrivateInterpreter(t *testing.T) {
	if runtimePathAllowed("/home/user/private-loader", []string{"/usr/bin"}) {
		t.Fatalf("runtimePathAllowed accepted private interpreter outside trusted roots")
	}
}
