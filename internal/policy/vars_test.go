package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestBuildVarsPlatformDirs(t *testing.T) {
	home := t.TempDir()
	vars, err := buildVars("/work", home, "/tmpdir", map[string]string{}, nil)
	if err != nil {
		t.Fatalf("buildVars: %v", err)
	}
	if runtime.GOOS == "darwin" {
		want := filepath.Join(home, "Library", "Application Support")
		if vars["CONFIG"] != want || vars["DATA"] != want || vars["STATE"] != want {
			t.Fatalf("vars = %#v", vars)
		}
		if vars["CACHE"] != filepath.Join(home, "Library", "Caches") {
			t.Fatalf("CACHE = %q", vars["CACHE"])
		}
		return
	}
	if vars["CONFIG"] != filepath.Join(home, ".config") || vars["DATA"] != filepath.Join(home, ".local", "share") {
		t.Fatalf("vars = %#v", vars)
	}
	if vars["XDG_CACHE_HOME"] != filepath.Join(home, ".cache") || vars["STATE"] != filepath.Join(home, ".local", "state") {
		t.Fatalf("vars = %#v", vars)
	}
}

func TestBuildVarsHonorsValidXDGEnv(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("linux only")
	}
	home := t.TempDir()
	custom := filepath.Join(home, "custom-config")
	vars, err := buildVars("/work", home, "/tmpdir", map[string]string{"XDG_CONFIG_HOME": custom}, nil)
	if err != nil {
		t.Fatalf("buildVars: %v", err)
	}
	if vars["CONFIG"] != custom {
		t.Fatalf("CONFIG = %q, want %q", vars["CONFIG"], custom)
	}
}

func TestBuildVarsIgnoresHostileEnvValues(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{
		"CARGO_HOME":      home,       // resolves to home: refused
		"GOPATH":          "/",        // filesystem root: refused
		"NVM_DIR":         "relative", // not absolute: refused
		"PYENV_ROOT":      filepath.Join(home, ".pyenv"),
		"XDG_CONFIG_HOME": home,
	}
	vars, err := buildVars("/work", home, "/tmpdir", env, nil)
	if err != nil {
		t.Fatalf("buildVars: %v", err)
	}
	for _, name := range []string{"CARGO_HOME", "GOPATH", "NVM_DIR"} {
		if _, ok := vars[name]; ok {
			t.Errorf("%s accepted hostile value %q", name, env[name])
		}
	}
	if vars["PYENV_ROOT"] != filepath.Join(home, ".pyenv") {
		t.Errorf("PYENV_ROOT = %q", vars["PYENV_ROOT"])
	}
	if runtime.GOOS != "darwin" && vars["CONFIG"] != filepath.Join(home, ".config") {
		t.Errorf("CONFIG = %q, want fallback after hostile XDG_CONFIG_HOME", vars["CONFIG"])
	}
}

func TestBuildVarsUserVars(t *testing.T) {
	home := t.TempDir()
	projects := filepath.Join(home, "repos")
	vars, err := buildVars("/work", home, "/tmpdir", map[string]string{}, map[string]string{"PROJECTS": projects})
	if err != nil {
		t.Fatalf("buildVars: %v", err)
	}
	if vars["PROJECTS"] != projects {
		t.Fatalf("PROJECTS = %q", vars["PROJECTS"])
	}
	for name, value := range map[string]string{
		"HOME":     "/elsewhere", // reserved
		"XDG_FOO":  "/elsewhere", // reserved prefix
		"lower":    "/elsewhere", // invalid name
		"BAD_ROOT": "/",          // refused value
	} {
		if _, err := buildVars("/work", home, "/tmpdir", map[string]string{}, map[string]string{name: value}); err == nil {
			t.Errorf("buildVars accepted %s=%s", name, value)
		}
	}
}

func TestResolveUsesConfigVarsAndVarFlag(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	shared := filepath.Join(root, "shared")
	flagged := filepath.Join(root, "flagged")
	for _, dir := range []string{home, project, shared, flagged} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{
		Profiles: map[string]config.Profile{"default": {Settings: config.Settings{
			PathSettings: config.PathSettings{ReadOnly: []string{"$SHARED", "$FLAGGED"}},
		}}},
		Vars: map[string]string{"SHARED": shared, "FLAGGED": "/overridden-by-flag"},
	}
	p, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Flags: cli.Flags{Var: []string{"FLAGGED=" + flagged}}},
		Global:    cfg,
		ParentEnv: map[string]string{},
		Home:      home,
		Tmp:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !containsString(p.ReadOnly, shared) || !containsString(p.ReadOnly, flagged) {
		t.Fatalf("ReadOnly = %#v", p.ReadOnly)
	}
}

func TestResolveTraceRecordsOutcomes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	for _, dir := range []string{home, project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{Profiles: map[string]config.Profile{"default": {Settings: config.Settings{
		PathSettings: config.PathSettings{ReadWrite: []string{"?$HOME/.does-not-exist", "+$HOME/.state/"}},
	}}}}
	p, err := Resolve(Inputs{Options: cli.Options{ProjectPath: project}, Global: cfg, ParentEnv: map[string]string{}, Home: home, Tmp: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var skipped, created bool
	for _, trace := range p.Trace {
		if trace.Raw == "?$HOME/.does-not-exist" && strings.HasPrefix(trace.Outcome, "skipped") {
			skipped = true
		}
		if trace.Raw == "+$HOME/.state/" && trace.Outcome == "created (dir)" {
			created = true
		}
	}
	if !skipped || !created {
		t.Fatalf("Trace = %#v", p.Trace)
	}
}

func TestBuildVarsExposesRuntimeDirWhenTheSessionHasOne(t *testing.T) {
	home := t.TempDir()
	vars, err := buildVars("/work", home, "/tmpdir", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if vars["XDG_RUNTIME_DIR"] != "/run/user/1000" {
		t.Errorf("XDG_RUNTIME_DIR = %q, want /run/user/1000", vars["XDG_RUNTIME_DIR"])
	}
}

func TestBuildVarsOmitsRuntimeDirWithoutOne(t *testing.T) {
	home := t.TempDir()
	// No fallback: there is no sensible default under $HOME, and inventing one
	// would name a path that exists nowhere.
	vars, err := buildVars("/work", home, "/tmpdir", map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := vars["XDG_RUNTIME_DIR"]; ok {
		t.Errorf("XDG_RUNTIME_DIR = %q, want it absent", vars["XDG_RUNTIME_DIR"])
	}
	// A hostile or nonsensical value is refused like any other path variable.
	vars, err = buildVars("/work", home, "/tmpdir", map[string]string{"XDG_RUNTIME_DIR": "relative/path"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := vars["XDG_RUNTIME_DIR"]; ok {
		t.Error("a relative XDG_RUNTIME_DIR was accepted")
	}
}
