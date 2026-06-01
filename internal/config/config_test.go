package config

import (
	"reflect"
	"runtime"
	"testing"
)

func TestLoadTOMLProfileInheritanceAndEnv(t *testing.T) {
	data := []byte(`
default_app = "claude"
default_profile = "secrets"
network = "none"

[profiles.default]
rw = ["$WORKSPACE"]
ro = ["$HOME/.cache/uv"]
env = ["PATH"]

[profiles.secrets]
inherits = "default"
env = ["OPENAI_API_KEY"]
network = "full"
allow_keychain = true
`)
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	profile, err := cfg.ResolveProfile("secrets")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if len(profile.ReadWrite) != 1 || profile.ReadWrite[0] != "$WORKSPACE" {
		t.Fatalf("ReadWrite = %#v", profile.ReadWrite)
	}
	if !contains(profile.Env, "PATH") || !contains(profile.Env, "OPENAI_API_KEY") {
		t.Fatalf("Env = %#v", profile.Env)
	}
	if profile.AllowKeychain == nil || !*profile.AllowKeychain {
		t.Fatalf("AllowKeychain = %#v, want true", profile.AllowKeychain)
	}
	if profile.Network != "full" {
		t.Fatalf("Network = %q, want child override full", profile.Network)
	}
}

func TestReplaceLists(t *testing.T) {
	parent := Profile{Settings: Settings{PathSettings: PathSettings{ReadOnly: []string{"a"}, ReadOnlyExec: []string{"bin"}}, Env: []string{"PATH"}}}
	child := Profile{Settings: Settings{PathSettings: PathSettings{ReplaceReadOnly: true, ReadOnly: []string{"b"}}, ReplaceEnv: true, Env: []string{"TERM"}}}
	got := MergeProfiles(parent, child)
	if len(got.ReadOnly) != 1 || got.ReadOnly[0] != "b" {
		t.Fatalf("ReadOnly = %#v", got.ReadOnly)
	}
	if len(got.ReadOnlyExec) != 1 || got.ReadOnlyExec[0] != "bin" {
		t.Fatalf("ReadOnlyExec = %#v", got.ReadOnlyExec)
	}
	if len(got.Env) != 1 || got.Env[0] != "TERM" {
		t.Fatalf("Env = %#v", got.Env)
	}
}

func TestMergeProfilesAllowsChildToOverrideNetwork(t *testing.T) {
	got := MergeProfiles(Profile{Settings: Settings{Network: "none"}}, Profile{Settings: Settings{Network: "full"}})
	if got.Network != "full" {
		t.Fatalf("Network = %q, want full", got.Network)
	}
}

func TestMergeProfilesAllowsChildToDisableKeychain(t *testing.T) {
	yes := true
	no := false
	got := MergeProfiles(Profile{Settings: Settings{AllowKeychain: &yes}}, Profile{Settings: Settings{AllowKeychain: &no}})
	if got.AllowKeychain == nil || *got.AllowKeychain {
		t.Fatalf("AllowKeychain = %#v, want false", got.AllowKeychain)
	}
}

func TestMergeConfigsDoesNotMutateParentProfiles(t *testing.T) {
	parent := DefaultConfig()
	child := Config{
		Profiles: map[string]Profile{
			"tool": {
				Settings: Settings{
					PathSettings: PathSettings{ReadWrite: []string{"/project-only"}},
					Env:          []string{"WORKSPACE_ONLY"},
				},
			},
		},
	}

	_ = MergeConfigs(parent, child)

	profile, err := parent.ResolveProfile("tool")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if contains(profile.ReadWrite, "/project-only") {
		t.Fatalf("parent tool profile was mutated: %#v", profile.ReadWrite)
	}
	if contains(profile.Env, "WORKSPACE_ONLY") {
		t.Fatalf("parent tool env was mutated: %#v", profile.Env)
	}
}

func TestMergeConfigsPreservesProfileInheritanceForPartialOverrides(t *testing.T) {
	parent := Config{
		Profiles: map[string]Profile{
			"agent": {
				Inherits: "tool",
				Settings: Settings{
					PathSettings: PathSettings{ReadWrite: []string{"state"}},
				},
			},
		},
	}
	child := Config{
		Profiles: map[string]Profile{
			"agent": {
				Settings: Settings{
					PathSettings: PathSettings{ReadOnly: []string{"prefs"}},
				},
			},
		},
	}

	got := MergeConfigs(parent, child)
	if got.Profiles["agent"].Inherits != "tool" {
		t.Fatalf("Inherits = %q, want tool", got.Profiles["agent"].Inherits)
	}
	profile := got.Profiles["agent"]
	if !contains(profile.ReadWrite, "state") || !contains(profile.ReadOnly, "prefs") {
		t.Fatalf("profile paths = %#v", profile.PathSettings)
	}
}

func TestDefaultConfigDefaultProfileHasNoFilesystemAccess(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("default")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if len(profile.ReadOnly) != 0 {
		t.Fatalf("ReadOnly = %#v", profile.ReadOnly)
	}
	if len(profile.ReadOnlyExec) != 0 {
		t.Fatalf("ReadOnlyExec = %#v", profile.ReadOnlyExec)
	}
	if len(profile.ReadWrite) != 0 {
		t.Fatalf("ReadWrite = %#v", profile.ReadWrite)
	}
	if len(profile.ReadWriteExec) != 0 {
		t.Fatalf("ReadWriteExec = %#v", profile.ReadWriteExec)
	}
}

func TestDefaultConfigIncludesToolProfile(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("tool")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if contains(profile.ReadWrite, "$WORKSPACE") || !contains(profile.ReadWrite, "$TMP/bulle/tmp") {
		t.Fatalf("ReadWrite = %#v", profile.ReadWrite)
	}
	if !contains(profile.Env, "PATH") {
		t.Fatalf("Env = %#v", profile.Env)
	}
	if contains(profile.Env, "HOME") || contains(profile.Env, "USER") || contains(profile.Env, "LOGNAME") {
		t.Fatalf("tool env should stay minimal, got %#v", profile.Env)
	}
	if !profile.AddExec || !profile.AddLibs {
		t.Fatalf("AddExec = %v, AddLibs = %v", profile.AddExec, profile.AddLibs)
	}
}

func TestDefaultConfigIncludesAgentProfiles(t *testing.T) {
	cfg := DefaultConfig()
	for name, tt := range map[string]struct {
		app       string
		statePath string
	}{
		"codex":    {app: "codex", statePath: "$HOME/.codex"},
		"claude":   {app: "claude", statePath: "$HOME/.claude"},
		"opencode": {app: "opencode", statePath: "$HOME/.config/opencode"},
		"pi":       {app: "pi", statePath: "$HOME/.pi"},
	} {
		profile, err := cfg.ResolveProfile(name)
		if err != nil {
			t.Fatalf("ResolveProfile(%q) returned error: %v", name, err)
		}
		if profile.DefaultApp != tt.app {
			t.Fatalf("profile %q DefaultApp = %q, want %q", name, profile.DefaultApp, tt.app)
		}
		if cfg.Profiles[name].Inherits != "tool" {
			t.Fatalf("profile %q Inherits = %q, want tool", name, cfg.Profiles[name].Inherits)
		}
		if contains(profile.ReadWrite, "$WORKSPACE") {
			t.Fatalf("profile %q should rely on automatic workspace grant, got ReadWrite: %#v", name, profile.ReadWrite)
		}
		if !contains(profile.Env, "HOME") || !contains(profile.Env, "USER") || !contains(profile.Env, "SHELL") {
			t.Fatalf("profile %q Env = %#v", name, profile.Env)
		}
		if name == "codex" && (!contains(profile.Env, "HTTPS_PROXY") || !contains(profile.Env, "NO_PROXY") || !contains(profile.Env, "SSL_CERT_FILE") || !contains(profile.Env, "CODEX_CONNECTORS_TOKEN") || !contains(profile.Env, "CODEX_CA_CERTIFICATE")) {
			t.Fatalf("codex Env missing network/MCP support variables: %#v", profile.Env)
		}
		if !contains(profile.ReadWriteExec, tt.statePath) {
			t.Fatalf("profile %q ReadWriteExec = %#v, want %q", name, profile.ReadWriteExec, tt.statePath)
		}
	}
}

func TestCodexProfileSupportsAppMCP(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("codex")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if profile.AllowKeychain == nil || !*profile.AllowKeychain {
		t.Fatalf("codex AllowKeychain = %#v, want true", profile.AllowKeychain)
	}
	assertMacOSPreferencesDefault(t, "codex", profile)
	if contains(profile.ReadWrite, "$HOME/Library/Keychains") {
		t.Fatalf("codex ReadWrite = %#v, did not want $HOME/Library/Keychains", profile.ReadWrite)
	}
}

func TestClaudeProfileDoesNotAllowKeychainByDefault(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("claude")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if profile.AllowKeychain != nil && *profile.AllowKeychain {
		t.Fatalf("claude AllowKeychain = %#v, want false", profile.AllowKeychain)
	}
	assertMacOSPreferencesDefault(t, "claude", profile)
	if contains(profile.ReadWrite, "$HOME/Library/Keychains") {
		t.Fatalf("claude ReadWrite = %#v, did not want $HOME/Library/Keychains", profile.ReadWrite)
	}
}

func TestClaudeProfileAllowsTopLevelClaudeState(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("claude")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if !contains(profile.ReadWrite, "$HOME/.claude.json") {
		t.Fatalf("ReadWrite = %#v", profile.ReadWrite)
	}
	if contains(profile.ReadWrite, "$HOME/.claude/settings.json") {
		t.Fatalf("ReadWrite = %#v, settings.json should be covered by ~/.claude", profile.ReadWrite)
	}
	for _, want := range []string{
		"$HOME/.claude",
		"$HOME/.local/share/claude",
	} {
		if !contains(profile.ReadWriteExec, want) {
			t.Fatalf("ReadWriteExec = %#v, want %q", profile.ReadWriteExec, want)
		}
	}
}

func TestOpenCodeProfileAllowsXDGState(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("opencode")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	for _, want := range []string{
		"$HOME/.config/opencode",
		"$HOME/.local/share/opencode",
		"$HOME/.local/state/opencode",
		"$HOME/.cache/opencode",
	} {
		if !contains(profile.ReadWriteExec, want) {
			t.Fatalf("ReadWriteExec = %#v, want %q", profile.ReadWriteExec, want)
		}
	}
}

func TestPiProfileAllowsExecutableAgentState(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("pi")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if !contains(profile.ReadWriteExec, "$HOME/.pi") {
		t.Fatalf("ReadWriteExec = %#v", profile.ReadWriteExec)
	}
}

func TestTopLevelConfigSettings(t *testing.T) {
	data := []byte(`
rw = ["$WORKSPACE/.venv"]
env = ["PYTHONPATH"]
network = "none"
`)
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	top := cfg.TopLevelProfile()
	if !contains(top.ReadWrite, "$WORKSPACE/.venv") || !contains(top.Env, "PYTHONPATH") || top.Network != "none" {
		t.Fatalf("top = %#v", top)
	}
}

func TestPlatformSettingsArePathOnly(t *testing.T) {
	settingsType := reflect.TypeOf(PlatformSettings{}.Exec)
	for _, field := range []string{
		"DefaultApp",
		"Env",
		"ReplaceEnv",
		"AddExec",
		"AddLibs",
		"AllowKeychain",
	} {
		if _, ok := settingsType.FieldByName(field); ok {
			t.Fatalf("PlatformSettings.Exec exposes profile-only field %s", field)
		}
	}
}

func assertMacOSPreferencesDefault(t *testing.T, name string, profile Profile) {
	t.Helper()
	hasPreferences := contains(profile.ReadOnly, "$HOME/Library/Preferences")
	if runtime.GOOS == "darwin" && !hasPreferences {
		t.Fatalf("profile %q ReadOnly = %#v, want /Library/Preferences on darwin", name, profile.ReadOnly)
	}
	if runtime.GOOS != "darwin" && hasPreferences {
		t.Fatalf("profile %q ReadOnly = %#v, did not want macOS preferences on %s", name, profile.ReadOnly, runtime.GOOS)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
