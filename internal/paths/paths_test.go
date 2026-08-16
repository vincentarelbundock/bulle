package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRejectsEmptyPath(t *testing.T) {
	for _, path := range []string{"", "   "} {
		t.Run(path, func(t *testing.T) {
			if _, err := ResolveList([]Input{{Path: path, Source: SourceUser}}, Vars{}); err == nil {
				t.Fatalf("ResolveList succeeded, want empty path error")
			}
		})
	}
}

func TestResolveExpandsVarsAndRejectsMissingUserPath(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveList([]Input{{Path: "$WORKSPACE", Source: SourceUser}, {Path: filepath.Join(tmp, "missing"), Source: SourceUser}}, Vars{
		"WORKSPACE": project,
		"HOME":      tmp,
		"TMPDIR":    tmp,
	})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want missing user path error")
	}
}

func TestResolveDropsMissingBuiltInPath(t *testing.T) {
	got, err := ResolveList([]Input{{Path: "/definitely/not/a/bulle/path", Source: SourceBuiltIn}}, Vars{})
	if err != nil {
		t.Fatalf("ResolveList returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}

func TestResolvePreservesBuiltInSymlinkAlias(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	alias := filepath.Join(tmp, "alias")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	wantReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveList([]Input{{Path: alias, Source: SourceBuiltIn}}, Vars{})
	if err != nil {
		t.Fatalf("ResolveList returned error: %v", err)
	}
	if len(got) != 2 || got[0] != alias || got[1] != wantReal {
		t.Fatalf("got %#v, want alias and real path", got)
	}
}

func TestResolvePreservesUserSymlinkAlias(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	alias := filepath.Join(tmp, "alias")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	wantReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveList([]Input{{Path: alias, Source: SourceUser}}, Vars{})
	if err != nil {
		t.Fatalf("ResolveList returned error: %v", err)
	}
	if len(got) != 2 || got[0] != alias || got[1] != wantReal {
		t.Fatalf("got %#v, want alias and real path", got)
	}
}

func TestResolveRejectsSymlinkToFilesystemRoot(t *testing.T) {
	tmp := t.TempDir()
	alias := filepath.Join(tmp, "escape")
	if err := os.Symlink("/", alias); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveList([]Input{{Path: alias, Source: SourceUser}}, Vars{})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want refusal for symlink to filesystem root")
	}
}

func TestResolveRejectsSymlinkToHomeDirectory(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(tmp, "escape")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveList([]Input{{Path: alias, Source: SourceUser}}, Vars{"HOME": home})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want refusal for symlink to home directory")
	}
}

func TestResolveRejectsSymlinkToSymlinkedHomeDirectory(t *testing.T) {
	tmp := t.TempDir()
	realHome := filepath.Join(tmp, "real-home")
	if err := os.Mkdir(realHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// HOME is itself a symlink to the physical home directory.
	homeLink := filepath.Join(tmp, "home-link")
	if err := os.Symlink(realHome, homeLink); err != nil {
		t.Fatal(err)
	}
	// A configured grant that resolves to the same physical directory by a
	// different path must still be refused.
	alias := filepath.Join(tmp, "escape")
	if err := os.Symlink(realHome, alias); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveList([]Input{{Path: alias, Source: SourceUser}}, Vars{"HOME": homeLink})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want refusal for grant resolving to symlinked home")
	}
}

func TestResolveRejectsUnknownEnvironmentVariables(t *testing.T) {
	tmp := t.TempDir()
	secret := filepath.Join(tmp, "secret")
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SECRET_PATH", secret)

	_, err := ResolveList([]Input{{Path: "$SECRET_PATH", Source: SourceUser}}, Vars{})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want unknown variable error")
	}
}

func TestResolveExpandsOnlyProvidedVars(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveList([]Input{{Path: "$WORKSPACE", Source: SourceUser}}, Vars{"WORKSPACE": project})
	if err != nil {
		t.Fatalf("ResolveList returned error: %v", err)
	}
	if !containsString(got, project) {
		t.Fatalf("got %#v, want %q", got, project)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestResolverNamespaceRecognizesResolverEntries(t *testing.T) {
	cases := []struct {
		raw       string
		namespace string
		argument  string
		ok        bool
	}{
		{"which:codex", "which", "codex", true},
		{"r:libs", "r", "libs", true},
		{"r:libs-user", "r", "libs-user", true},
		{"uv:cache", "uv", "cache", true},
		// A leading separator, dot, tilde, or variable means a path, never a
		// resolver: this is the escape hatch for literal paths with a colon.
		{"./ruby:gems", "", "", false},
		{"/tmp/ruby:gems", "", "", false},
		{"~/ruby:gems", "", "", false},
		{"$HOME/a:b", "", "", false},
		// Uppercase and empty namespaces are paths, not resolvers.
		{"R:libs", "", "", false},
		{":libs", "", "", false},
		{"/tmp/plain", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		namespace, argument, ok := ResolverNamespace(tc.raw)
		if ok != tc.ok || namespace != tc.namespace || argument != tc.argument {
			t.Errorf("ResolverNamespace(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.raw, namespace, argument, ok, tc.namespace, tc.argument, tc.ok)
		}
	}
}

func TestResolveRejectsSymlinkToAncestorOfHomeDirectory(t *testing.T) {
	tmp := t.TempDir()
	homes := filepath.Join(tmp, "homes")
	home := filepath.Join(homes, "user")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(tmp, "escape")
	if err := os.Symlink(homes, alias); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveList([]Input{{Path: alias, Source: SourceUser}}, Vars{"HOME": home})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want refusal for a symlink to the directory holding every home")
	}
}

// A variable's value is data, not configuration: a dollar sign inside it must
// stay a dollar sign rather than being reinterpreted on a second pass.
func TestResolveExpandsVariableValuesOnlyOnce(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hostile := range []string{"/${NOPE:-}", "/$HOME"} {
		vars := Vars{"HOME": home, "RUSTUP_HOME": hostile}
		got, err := ResolveList([]Input{{Path: "?$RUSTUP_HOME", Source: SourceUser, Optional: true}}, vars)
		if err != nil {
			continue // refused outright is also fine
		}
		for _, path := range got {
			if path == "/" || path == filepath.Clean(home) {
				t.Fatalf("RUSTUP_HOME=%q granted %q", hostile, path)
			}
		}
	}
}

func TestResolveRefusesEntryThatExpandsToTheFilesystemRoot(t *testing.T) {
	_, err := ResolveList([]Input{{Path: "${FOO:-}/", Source: SourceUser}}, Vars{"HOME": "/home/u"})
	if err == nil {
		t.Fatalf("ResolveList succeeded, want refusal for an entry expanding to /")
	}
}

// A ".." component is collapsed lexically, but the kernel resolves a symlinked
// component first, so only the path the kernel reaches may be granted.
func TestResolveDoesNotGrantTheLexicalParentAcrossASymlink(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "other", "real")
	secret := filepath.Join(tmp, "b")
	if err := os.MkdirAll(filepath.Join(tmp, "other", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(tmp, "link")); err != nil {
		t.Fatal(err)
	}
	// Not filepath.Join: it would clean the ".." away before resolution ever saw it.
	entry := tmp + "/link/../b"
	got, err := ResolveList([]Input{{Path: entry, Source: SourceUser}}, Vars{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range got {
		if path == secret {
			t.Fatalf("granted the lexically-cleaned %q, which the entry never traverses: %v", secret, got)
		}
	}
}
