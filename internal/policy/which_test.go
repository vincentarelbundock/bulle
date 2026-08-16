package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

// fakeTool installs an executable named name under root/pkg/vX/bin plus a
// version-manager-style symlink in root/shims, and returns the shim path's
// directory (for PATH) and the real binary path.
func fakeTool(t *testing.T, root string, name string) (pathDir string, real string) {
	t.Helper()
	binDir := filepath.Join(root, "pkg", "v1", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	real = filepath.Join(binDir, name)
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pathDir = filepath.Join(root, "shims")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(pathDir, name)); err != nil {
		t.Fatal(err)
	}
	return pathDir, real
}

func whichTestInputs(t *testing.T, cfg config.Config, parentPATH string) Inputs {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return Inputs{
		Options:   cli.Options{ProjectPath: project},
		Global:    cfg,
		ParentEnv: map[string]string{"PATH": parentPATH},
		Home:      root,
		Tmp:       t.TempDir(),
	}
}

func profileWith(settings config.Settings) config.Config {
	return config.Config{Profiles: map[string]config.Profile{"default": {Settings: settings}}}
}

func TestResolveWhichEntryGrantsBinaryAndShim(t *testing.T) {
	tools := t.TempDir()
	pathDir, real := fakeTool(t, tools, "codex")
	alias := filepath.Join(pathDir, "codex")
	cfg := profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnlyExec: []string{"which:codex"}}})
	in := whichTestInputs(t, cfg, pathDir)
	p, err := Resolve(in)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer os.RemoveAll(p.ShimDir)
	if p.ShimDir == "" {
		t.Fatalf("ShimDir empty")
	}
	for _, want := range []string{alias, real, p.ShimDir} {
		if !containsString(p.ReadOnlyExec, want) {
			t.Errorf("ReadOnlyExec missing %q: %#v", want, p.ReadOnlyExec)
		}
	}
	// The containing bin directories must NOT be granted: that is the whole
	// point of the shim design.
	for _, banned := range []string{pathDir, filepath.Dir(real)} {
		if containsString(p.ReadOnlyExec, banned) {
			t.Errorf("ReadOnlyExec grants directory %q: %#v", banned, p.ReadOnlyExec)
		}
	}
	link, err := os.Readlink(filepath.Join(p.ShimDir, "codex"))
	if err != nil || link != alias {
		t.Fatalf("shim link = %q, %v; want alias %q", link, err, alias)
	}
	if got := p.Env["PATH"]; !strings.HasPrefix(got, p.ShimDir) {
		t.Fatalf("PATH = %q, want shim dir prefix", got)
	}
}

func TestResolvePkgEntryGrantsPackageRoot(t *testing.T) {
	tools := t.TempDir()
	pathDir, real := fakeTool(t, tools, "node")
	cfg := profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnlyExec: []string{"pkg:node"}}})
	p, err := Resolve(whichTestInputs(t, cfg, pathDir))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer os.RemoveAll(p.ShimDir)
	// The package root is granted by its resolved spelling: a grant names an
	// inode, and on macOS every temporary directory is reached through a
	// symlink (/var -> /private/var).
	pkgRoot := filepath.Dir(filepath.Dir(real))
	if resolved, err := filepath.EvalSymlinks(pkgRoot); err == nil {
		pkgRoot = resolved
	}
	if !containsString(p.ReadOnlyExec, pkgRoot) {
		t.Fatalf("ReadOnlyExec missing package root %q: %#v", pkgRoot, p.ReadOnlyExec)
	}
}

func TestPackageRootForRefusesSystemRoots(t *testing.T) {
	home := t.TempDir()
	for _, binary := range []string{"/usr/local/bin/faketool", "/usr/bin/faketool", "/bin/faketool"} {
		if _, err := packageRootFor(binary, home); err == nil || !strings.Contains(err.Error(), "system directory") {
			t.Errorf("packageRootFor(%q) err = %v, want system-directory refusal", binary, err)
		}
	}
	if _, err := packageRootFor(filepath.Join(home, ".mise", "installs", "node", "22", "bin", "node"), home); err != nil {
		t.Errorf("packageRootFor legitimate tree: %v", err)
	}
}

func TestResolveOptionalWhichMissingSkips(t *testing.T) {
	cfg := profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnlyExec: []string{"?which:no-such-tool-xyz"}}})
	p, err := Resolve(whichTestInputs(t, cfg, t.TempDir()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.ShimDir != "" {
		t.Fatalf("ShimDir = %q, want empty", p.ShimDir)
	}
}

func TestResolveRequiredWhichMissingFails(t *testing.T) {
	cfg := profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnlyExec: []string{"which:no-such-tool-xyz"}}})
	_, err := Resolve(whichTestInputs(t, cfg, t.TempDir()))
	if err == nil {
		t.Fatalf("Resolve succeeded, want missing which target error")
	}
}

func TestResolveRejectsWhichInReadOnlyList(t *testing.T) {
	cfg := profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnly: []string{"which:codex"}}})
	_, err := Resolve(whichTestInputs(t, cfg, t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "only valid in rox and rwx") {
		t.Fatalf("err = %v, want resolver-list rejection", err)
	}
}

// A pkg: entry on a machine that installs the tool in a system directory has
// no package tree to grant. An optional entry degrades to the binary alone,
// because "?" means "grant this where it makes sense", not "fail elsewhere".
func TestResolveOptionalPkgWithSystemRootGrantsTheBinaryOnly(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	binDir := filepath.Join(root, "bin")
	for _, dir := range []string{project, binDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(binDir, "faketool")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The package root two levels up is the home directory itself, which is
	// refused for the same reason /usr is.
	inputs := Inputs{
		Options:   cli.Options{ProjectPath: project},
		Global:    profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnlyExec: []string{"?pkg:faketool"}}}),
		ParentEnv: map[string]string{"PATH": binDir},
		Home:      root,
		Tmp:       t.TempDir(),
	}
	p, err := Resolve(inputs)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer os.RemoveAll(p.ShimDir)
	if !containsString(p.ReadOnlyExec, binary) {
		t.Fatalf("ReadOnlyExec missing the binary %q: %#v", binary, p.ReadOnlyExec)
	}
	if containsString(p.ReadOnlyExec, filepath.Clean(root)) {
		t.Fatalf("ReadOnlyExec granted the refused package root: %#v", p.ReadOnlyExec)
	}

	// Without the marker the author asked for the package tree, so say so.
	inputs.Global = profileWith(config.Settings{PathSettings: config.PathSettings{ReadOnlyExec: []string{"pkg:faketool"}}})
	if _, err := Resolve(inputs); err == nil || !strings.Contains(err.Error(), "system directory") {
		t.Fatalf("err = %v, want a system-directory refusal for the required entry", err)
	}
}
