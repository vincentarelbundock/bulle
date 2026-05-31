package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestResolveRejectsHomeProject(t *testing.T) {
	home := t.TempDir()
	opts := cli.Options{ProjectPath: home}
	_, err := Resolve(Inputs{
		Options:   opts,
		Global:    config.Config{DefaultProfile: "default", Profiles: map[string]config.Profile{"default": {}}},
		ParentEnv: map[string]string{"HOME": home, "PATH": "/usr/bin"},
		Home:      home,
		Tmp:       t.TempDir(),
	})
	if err == nil {
		t.Fatalf("Resolve succeeded, want home rejection")
	}
}

func TestResolveRejectsReservedPathVarOverride(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DefaultProfile: "default",
		Vars:           map[string]string{"HOME": filepath.Join(root, "fake-home")},
		Profiles:       map[string]config.Profile{"default": {}},
	}

	_, err := Resolve(Inputs{Options: cli.Options{ProjectPath: project}, Global: cfg, ParentEnv: map[string]string{}, Home: root, Tmp: t.TempDir()})
	if err == nil {
		t.Fatalf("Resolve succeeded, want reserved var rejection")
	}
}

func TestResolveMergesDefaultsConfigAndCLI(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	cache := filepath.Join(root, "cache")
	for _, path := range []string{project, cache} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	opts := cli.Options{ProjectPath: project, Command: []string{"bash"}, Flags: cli.Flags{ReadOnly: []string{cache}, ReadOnlyExec: []string{"/usr/bin"}, Env: []string{"PATH"}}}
	cfg := config.Config{
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{"default": {
			Settings: config.Settings{
				PathSettings: config.PathSettings{ReadWrite: []string{"$WORKSPACE"}},
				Env:          []string{"HOME"},
			},
		}},
	}
	got, err := Resolve(Inputs{Options: opts, Global: cfg, ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"}, Home: root, Tmp: root})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectPath != wantProject {
		t.Fatalf("ProjectPath = %q", got.ProjectPath)
	}
	if !containsString(got.ReadWrite, wantProject) {
		t.Fatalf("ReadWrite = %#v", got.ReadWrite)
	}
	wantCache, err := filepath.EvalSymlinks(cache)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(got.ReadOnly, wantCache) {
		t.Fatalf("ReadOnly = %#v", got.ReadOnly)
	}
	if got.Env["PATH"] != "/usr/bin" || got.Env["HOME"] != root {
		t.Fatalf("Env = %#v", got.Env)
	}
}

func TestResolveAlwaysUsesRuntimeBackend(t *testing.T) {
	project := t.TempDir()
	cfg := config.Config{DefaultProfile: "default", Profiles: map[string]config.Profile{"default": {}}}
	got, err := Resolve(Inputs{Options: cli.Options{ProjectPath: project}, Global: cfg, ParentEnv: map[string]string{}, Home: t.TempDir(), Tmp: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.Backend != RuntimeDefaultBackend() {
		t.Fatalf("Backend = %q, want %q", got.Backend, RuntimeDefaultBackend())
	}
}

func TestResolveDefaultProfileGrantsWorkspaceReadWrite(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"bash"}},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(got.ReadWrite, wantProject) {
		t.Fatalf("ReadWrite = %#v, want %q", got.ReadWrite, wantProject)
	}
}

func TestResolveNoWorkspaceDisablesAutomaticWorkspaceGrant(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"bash"}, Flags: cli.Flags{NoWorkspace: true}},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(got.ReadOnly) != 0 || len(got.ReadOnlyExec) != 0 || len(got.ReadWrite) != 0 || len(got.ReadWriteExec) != 0 {
		t.Fatalf("filesystem policy = ro:%#v rox:%#v rw:%#v rwx:%#v", got.ReadOnly, got.ReadOnlyExec, got.ReadWrite, got.ReadWriteExec)
	}
}

func TestResolveExplicitProjectPathGrantsReadWriteWorkspace(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options: cli.Options{
			ProjectPath: project,
			Command:     []string{"bash"},
		},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(got.ReadWrite, wantProject) {
		t.Fatalf("ReadWrite = %#v, want %q", got.ReadWrite, wantProject)
	}
}

func TestResolveAddExecWithoutExplicitProjectGrantsCurrentProject(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options: cli.Options{
			ProjectPath: project,
			Command:     []string{"ls"},
			Flags:       cli.Flags{AddExec: true},
		},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(got.ReadWrite, wantProject) {
		t.Fatalf("ReadWrite = %#v, want %q", got.ReadWrite, wantProject)
	}
}

func TestResolveToolProfileIncludesToolAndPlatformDefaults(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	localBin := filepath.Join(root, ".local", "bin")
	dotBin := filepath.Join(root, ".bin")
	toolTmp := filepath.Join(tmp, "bulle", "tmp")
	for _, path := range []string{project, toolTmp, localBin, dotBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options: cli.Options{ProjectPath: project, Command: []string{"bash"}, Flags: cli.Flags{Profile: "tool"}},
		Global:  config.DefaultConfig(),
		ParentEnv: map[string]string{
			"HOME":    root,
			"PATH":    "/usr/bin" + string(os.PathListSeparator) + localBin + string(os.PathListSeparator) + dotBin,
			"USER":    "vincent",
			"LOGNAME": "vincent",
		},
		Home: root,
		Tmp:  tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	wantTmp, err := filepath.EvalSymlinks(toolTmp)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(got.ReadWrite, wantProject) || !containsString(got.ReadWrite, wantTmp) {
		t.Fatalf("ReadWrite = %#v, want project and tmp", got.ReadWrite)
	}
	for _, key := range []string{"BUN_TMPDIR", "TMPDIR", "TMP", "TEMP"} {
		if got.Env[key] != wantTmp {
			t.Fatalf("Env[%s] = %q, want %q", key, got.Env[key], wantTmp)
		}
	}
	if _, ok := got.Env["USER"]; ok {
		t.Fatalf("tool Env should not include USER: %#v", got.Env)
	}
	if _, ok := got.Env["LOGNAME"]; ok {
		t.Fatalf("tool Env should not include LOGNAME: %#v", got.Env)
	}
	if !containsString(got.ReadOnlyExec, "/usr/bin") {
		t.Fatalf("ReadOnlyExec = %#v, want /usr/bin", got.ReadOnlyExec)
	}
	for _, want := range []string{localBin, dotBin} {
		if !containsString(got.ReadOnlyExec, want) {
			t.Fatalf("ReadOnlyExec = %#v, want %q", got.ReadOnlyExec, want)
		}
		if !pathListContains(got.Env["PATH"], want) {
			t.Fatalf("PATH = %q, want %q", got.Env["PATH"], want)
		}
	}
}

func TestResolveDoesNotInferAgentProfileFromExplicitCommand(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	codexState := filepath.Join(root, ".codex")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp"), codexState} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"codex"}},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantCodexState, err := filepath.EvalSymlinks(codexState)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(got.ReadWriteExec, wantCodexState) {
		t.Fatalf("ReadWriteExec = %#v, did not want %q", got.ReadWriteExec, wantCodexState)
	}
}

func TestResolveExplicitAgentProfileGrantsAgentState(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	codexState := filepath.Join(root, ".codex")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp"), codexState, filepath.Join(root, "Library", "Keychains"), filepath.Join(root, "Library", "Preferences")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"bash"}, Flags: cli.Flags{Profile: "codex"}},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantCodexState, err := filepath.EvalSymlinks(codexState)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(got.ReadWriteExec, wantCodexState) {
		t.Fatalf("ReadWriteExec = %#v, want %q", got.ReadWriteExec, wantCodexState)
	}
}

func TestResolveClaudeProfileDoesNotFollowUnlistedConfigSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	claudeState := filepath.Join(root, ".claude")
	settingsTarget := filepath.Join(root, "dotfiles", ".claude", "settings.json")
	skillsTarget := filepath.Join(root, "dotfiles", ".claude", "skills")
	for _, path := range []string{
		project,
		filepath.Join(tmp, "bulle", "tmp"),
		filepath.Join(root, "Library", "Keychains"),
		filepath.Join(root, "Library", "Preferences"),
		filepath.Join(root, ".local", "share", "claude"),
		claudeState,
		filepath.Dir(settingsTarget),
		skillsTarget,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsTarget, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(settingsTarget, filepath.Join(claudeState, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillsTarget, filepath.Join(claudeState, "skills")); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"claude"}, Flags: cli.Flags{Profile: "claude"}},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantSettings, err := filepath.EvalSymlinks(settingsTarget)
	if err != nil {
		t.Fatal(err)
	}
	wantSkills, err := filepath.EvalSymlinks(skillsTarget)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(got.ReadWrite, wantSettings) {
		t.Fatalf("ReadWrite = %#v, did not want unlisted symlink target %q", got.ReadWrite, wantSettings)
	}
	if containsString(got.ReadWriteExec, wantSkills) {
		t.Fatalf("ReadWriteExec = %#v, did not want unlisted symlink target %q", got.ReadWriteExec, wantSkills)
	}
}

func TestResolveClaudeProfileDoesNotAllowKeychain(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{
		project,
		filepath.Join(root, ".claude"),
		filepath.Join(root, ".claude", "skills"),
		filepath.Join(root, ".local", "share", "claude"),
		filepath.Join(root, "Library", "Keychains"),
		filepath.Join(root, "Library", "Preferences"),
		filepath.Join(tmp, "bulle", "tmp"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"claude"}, Flags: cli.Flags{Profile: "claude"}},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.AllowKeychain {
		t.Fatalf("AllowKeychain = true, want false")
	}
	keychains := filepath.Join(root, "Library", "Keychains")
	realKeychains, err := filepath.EvalSymlinks(keychains)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(got.ReadWrite, realKeychains) {
		t.Fatalf("ReadWrite = %#v, did not want %q", got.ReadWrite, realKeychains)
	}
}

func TestResolveBuiltInAgentProfiles(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	paths := []string{
		project,
		filepath.Join(tmp, "bulle", "tmp"),
		filepath.Join(root, ".codex"),
		filepath.Join(root, "Library", "Keychains"),
		filepath.Join(root, "Library", "Preferences"),
		filepath.Join(root, ".claude"),
		filepath.Join(root, ".local", "share", "claude"),
		filepath.Join(root, ".config", "opencode"),
		filepath.Join(root, ".local", "share", "opencode"),
		filepath.Join(root, ".local", "state", "opencode"),
		filepath.Join(root, ".cache", "opencode"),
		filepath.Join(root, ".pi"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"codex", "claude", "opencode", "pi"} {
		t.Run(name, func(t *testing.T) {
			got, err := Resolve(Inputs{
				Options: cli.Options{ProjectPath: project, Command: []string{"agent"}, Flags: cli.Flags{Profile: name}},
				Global:  config.DefaultConfig(),
				ParentEnv: map[string]string{
					"HOME":                   root,
					"PATH":                   "/usr/bin",
					"USER":                   "vincent",
					"TERM":                   "xterm-256color",
					"LANG":                   "en_CA.UTF-8",
					"SHELL":                  "/bin/zsh",
					"CODEX_CONNECTORS_TOKEN": "connector-secret",
				},
				Home: root,
				Tmp:  tmp,
			})
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			for _, key := range []string{"HOME", "PATH", "USER", "TERM", "LANG", "SHELL"} {
				if got.Env[key] == "" {
					t.Fatalf("Env[%s] is empty in %#v", key, got.Env)
				}
			}
			if name == "codex" && got.Env["CODEX_CONNECTORS_TOKEN"] != "connector-secret" {
				t.Fatalf("codex Env[CODEX_CONNECTORS_TOKEN] = %q", got.Env["CODEX_CONNECTORS_TOKEN"])
			}
			if name == "codex" && !got.AllowKeychain {
				t.Fatalf("AllowKeychain = false, want true")
			}
			if name != "codex" && got.AllowKeychain {
				t.Fatalf("AllowKeychain = true, want false")
			}
		})
	}
}

func TestResolveConfiguredDefaultProfileDoesNotUseCommandProfile(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	codexState := filepath.Join(root, ".codex")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp"), codexState} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.DefaultProfile = "strict"
	cfg.Profiles["strict"] = config.Profile{Inherits: "default"}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"codex"}},
		Global:    cfg,
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	wantCodexState, err := filepath.EvalSymlinks(codexState)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(got.ReadWriteExec, wantCodexState) {
		t.Fatalf("ReadWriteExec = %#v, did not want %q", got.ReadWriteExec, wantCodexState)
	}
}

func TestResolveDefersPATHSanitizationForAddExecCommandLookup(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	binDir := filepath.Join(root, "bin")
	for _, path := range []string{project, binDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{"default": {
			Settings: config.Settings{ReplaceEnv: true, Env: []string{"PATH"}},
		}},
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"tool"}, Flags: cli.Flags{AddExec: true}},
		Global:    cfg,
		ParentEnv: map[string]string{"PATH": binDir},
		Home:      root,
		Tmp:       root,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.Env["PATH"] != binDir {
		t.Fatalf("PATH = %q, want deferred sanitization to keep %q", got.Env["PATH"], binDir)
	}
}

func TestResolveSanitizesPATHToExecutableRoots(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	allowedBin := filepath.Join(root, "allowed", "bin")
	deniedBin := filepath.Join(root, "denied", "bin")
	notDir := filepath.Join(root, "allowed", "not-dir")
	for _, path := range []string{project, allowedBin, deniedBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(notDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{"default": {
			Settings: config.Settings{
				PathSettings: config.PathSettings{
					ReadWrite:    []string{"$WORKSPACE"},
					ReadOnlyExec: []string{allowedBin},
				},
				ReplaceEnv: true,
				Env:        []string{"PATH"},
			},
		}},
	}
	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"tool"}},
		Global:    cfg,
		ParentEnv: map[string]string{"PATH": deniedBin + string(os.PathListSeparator) + notDir + string(os.PathListSeparator) + allowedBin},
		Home:      root,
		Tmp:       root,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.Env["PATH"] != allowedBin {
		t.Fatalf("PATH = %q, want %q", got.Env["PATH"], allowedBin)
	}
}

func TestResolveSanitizesPATHRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	allowed := filepath.Join(root, "allowed")
	outsideBin := filepath.Join(root, "outside", "bin")
	for _, path := range []string{project, allowed, outsideBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	escapingBin := filepath.Join(allowed, "bin")
	if err := os.Symlink(outsideBin, escapingBin); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{"default": {
			Settings: config.Settings{
				PathSettings: config.PathSettings{ReadOnlyExec: []string{allowed}},
				ReplaceEnv:   true,
				Env:          []string{"PATH"},
			},
		}},
	}

	got, err := Resolve(Inputs{
		Options:   cli.Options{ProjectPath: project, Command: []string{"tool"}},
		Global:    cfg,
		ParentEnv: map[string]string{"PATH": escapingBin},
		Home:      root,
		Tmp:       root,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.Env["PATH"] != "" {
		t.Fatalf("PATH = %q, want symlink escape removed", got.Env["PATH"])
	}
}

func TestResolvePassesExplicitSecretEnvWithoutDenyList(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options: cli.Options{
			ProjectPath: project,
			Command:     []string{"codex"},
			Flags:       cli.Flags{Profile: "tool", Env: []string{"OPENAI_API_KEY"}},
		},
		Global: config.DefaultConfig(),
		ParentEnv: map[string]string{
			"HOME":           root,
			"PATH":           "/usr/bin",
			"OPENAI_API_KEY": "secret",
		},
		Home: root,
		Tmp:  tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.Env["OPENAI_API_KEY"] != "secret" {
		t.Fatalf("Env = %#v, want OPENAI_API_KEY", got.Env)
	}
}

func TestResolveAddLibsIncludesMacOSRuntimeRootsWithoutToolDefaults(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS runtime roots are platform-specific")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tmp := filepath.Join(root, "tmp")
	for _, path := range []string{project, filepath.Join(tmp, "bulle", "tmp")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(Inputs{
		Options: cli.Options{
			ProjectPath: project,
			Command:     []string{"tool"},
			Flags:       cli.Flags{AddLibs: true},
		},
		Global:    config.DefaultConfig(),
		ParentEnv: map[string]string{"HOME": root, "PATH": "/usr/bin"},
		Home:      root,
		Tmp:       tmp,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !containsString(got.ReadOnly, "/System/Library") {
		t.Fatalf("ReadOnly = %#v, want /System/Library", got.ReadOnly)
	}
	if !containsString(got.ReadOnlyExec, "/usr/lib") {
		t.Fatalf("ReadOnlyExec = %#v, want /usr/lib", got.ReadOnlyExec)
	}
	if containsString(got.ReadOnlyExec, "/bin") {
		t.Fatalf("ReadOnlyExec = %#v, did not want full tool executable roots", got.ReadOnlyExec)
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

func pathListContains(value string, want string) bool {
	for _, path := range filepath.SplitList(value) {
		if path == want {
			return true
		}
	}
	return false
}
