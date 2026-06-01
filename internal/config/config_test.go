package config

import (
	"bytes"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadTOMLProfileInheritanceEnvAndMachLookup(t *testing.T) {
	data := []byte(`
default_app = "claude"
[profiles.default]
rw = ["$WORKSPACE"]
ro = ["$HOME/.cache/uv"]
env = ["PATH"]
deny = ["network"]

[profiles.secrets]
inherits = "default"
env = ["OPENAI_API_KEY"]
allow = ["network"]
mach_lookup = ["com.example.agent"]
`)
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	profile, err := cfg.ResolveProfile("secrets")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Profiles["secrets"].Inherits.Names, []string{"default"}) {
		t.Fatalf("Inherits = %#v, want default", cfg.Profiles["secrets"].Inherits)
	}
	if len(profile.ReadWrite) != 1 || profile.ReadWrite[0] != "$WORKSPACE" {
		t.Fatalf("ReadWrite = %#v", profile.ReadWrite)
	}
	if !contains(profile.Env, "PATH") || !contains(profile.Env, "OPENAI_API_KEY") {
		t.Fatalf("Env = %#v", profile.Env)
	}
	if !contains(profile.MachLookup, "com.example.agent") {
		t.Fatalf("MachLookup = %#v, want com.example.agent", profile.MachLookup)
	}
	if !contains(profile.Allow, "network") || contains(profile.Deny, "network") {
		t.Fatalf("capabilities = allow %#v deny %#v, want network allowed", profile.Allow, profile.Deny)
	}
}

func TestLoadRejectsDefaultProfileAndVars(t *testing.T) {
	for name, data := range map[string]string{
		"default_profile": `default_profile = "tool"`,
		"vars": `[vars]
CACHE = "$HOME/.cache"
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte(data))
			if err == nil {
				t.Fatalf("LoadBytes succeeded, want %s rejected", name)
			}
		})
	}
}

func TestLoadTOMLProfileMultipleInheritanceMergesSequentially(t *testing.T) {
	data := []byte(`
[profiles.base]
ro = ["/base"]
env = ["PATH=/bin"]

[profiles.extra]
rw = ["/extra"]
env = ["PATH=/opt/bin", "TERM"]

[profiles.agent]
inherits = ["base", "extra"]
env = ["PATH=/agent/bin"]
`)
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	profile, err := cfg.ResolveProfile("agent")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	if !contains(profile.ReadOnly, "/base") || !contains(profile.ReadWrite, "/extra") {
		t.Fatalf("profile paths = %#v", profile.PathSettings)
	}
	if contains(profile.Env, "PATH=/bin") || contains(profile.Env, "PATH=/opt/bin") || !contains(profile.Env, "PATH=/agent/bin") || !contains(profile.Env, "TERM") {
		t.Fatalf("Env = %#v", profile.Env)
	}
}

func TestMergeProfilesPromotesPathRightsAndSupersedesEnv(t *testing.T) {
	parent := Profile{Settings: Settings{
		PathSettings: PathSettings{ReadOnlyExec: []string{"/tool"}},
		Env:          []string{"PATH=/bin", "TERM"},
		MachLookup:   []string{"com.example.parent"},
	}}
	child := Profile{Settings: Settings{
		PathSettings: PathSettings{ReadOnly: []string{"/prefs"}, ReadWrite: []string{"/tool"}},
		Env:          []string{"PATH=/custom/bin", "HOME"},
		MachLookup:   []string{"com.example.parent", "com.example.child"},
	}}
	got := MergeProfiles(parent, child)
	if !contains(got.ReadWriteExec, "/tool") || contains(got.ReadOnlyExec, "/tool") || contains(got.ReadWrite, "/tool") {
		t.Fatalf("PathSettings = %#v, want /tool promoted to rwx", got.PathSettings)
	}
	if !contains(got.ReadOnly, "/prefs") {
		t.Fatalf("ReadOnly = %#v, want /prefs", got.ReadOnly)
	}
	if contains(got.Env, "PATH=/bin") || !contains(got.Env, "PATH=/custom/bin") || !contains(got.Env, "TERM") || !contains(got.Env, "HOME") {
		t.Fatalf("Env = %#v", got.Env)
	}
	if !reflect.DeepEqual(got.MachLookup, []string{"com.example.parent", "com.example.child"}) {
		t.Fatalf("MachLookup = %#v", got.MachLookup)
	}
}

func TestMergeProfilesAllowsChildToOverrideCapabilities(t *testing.T) {
	got := MergeProfiles(Profile{Settings: Settings{Allow: []string{"network"}}}, Profile{Settings: Settings{Deny: []string{"network"}}})
	if contains(got.Allow, "network") || !contains(got.Deny, "network") {
		t.Fatalf("capabilities = allow %#v deny %#v, want network denied", got.Allow, got.Deny)
	}

	got = MergeProfiles(Profile{Settings: Settings{Deny: []string{"network"}}}, Profile{Settings: Settings{Allow: []string{"network"}}})
	if !contains(got.Allow, "network") || contains(got.Deny, "network") {
		t.Fatalf("capabilities = allow %#v deny %#v, want network allowed", got.Allow, got.Deny)
	}
}

func TestMergeProfilesAllowsChildToDenyMachLookup(t *testing.T) {
	got := MergeProfiles(Profile{Settings: Settings{MachLookup: []string{"com.example.dns", "com.example.keychain"}}}, Profile{Settings: Settings{DenyMachLookup: []string{"com.example.dns"}}})
	if contains(got.MachLookup, "com.example.dns") || !contains(got.DenyMachLookup, "com.example.dns") || !contains(got.MachLookup, "com.example.keychain") {
		t.Fatalf("MachLookup = %#v DenyMachLookup = %#v, want dns denied and keychain retained", got.MachLookup, got.DenyMachLookup)
	}

	got = MergeProfiles(Profile{Settings: Settings{DenyMachLookup: []string{"com.example.dns"}}}, Profile{Settings: Settings{MachLookup: []string{"com.example.dns"}}})
	if !contains(got.MachLookup, "com.example.dns") || contains(got.DenyMachLookup, "com.example.dns") {
		t.Fatalf("MachLookup = %#v DenyMachLookup = %#v, want child allow to supersede deny", got.MachLookup, got.DenyMachLookup)
	}
}

func TestMergeProfilesAllowsChildToDisableBooleanSetting(t *testing.T) {
	yes := true
	no := false
	got := MergeProfiles(Profile{Settings: Settings{AddExec: &yes}}, Profile{Settings: Settings{AddExec: &no}})
	if got.AddExec == nil || *got.AddExec {
		t.Fatalf("AddExec = %#v, want false", got.AddExec)
	}
}

func TestEffectiveProfileMergesCommaSeparatedProfilesSequentially(t *testing.T) {
	cfg := Config{Profiles: map[string]Profile{
		"default": {},
		"agent": {
			Settings: Settings{
				DefaultApp: "agent",
				Env:        []string{"PATH=/bin"},
				Allow:      []string{"network"},
			},
		},
		"offline": {
			Settings: Settings{
				Env:  []string{"PATH=/offline", "TERM"},
				Deny: []string{"network"},
			},
		},
	}}

	profile, name, _, err := EffectiveProfile(cfg, "agent, offline")
	if err != nil {
		t.Fatalf("EffectiveProfile returned error: %v", err)
	}
	if name != "agent,offline" {
		t.Fatalf("name = %q, want normalized comma profile list", name)
	}
	if profile.DefaultApp != "agent" {
		t.Fatalf("DefaultApp = %q, want agent", profile.DefaultApp)
	}
	if contains(profile.Allow, "network") || !contains(profile.Deny, "network") {
		t.Fatalf("capabilities = allow %#v deny %#v, want offline to override network", profile.Allow, profile.Deny)
	}
	if contains(profile.Env, "PATH=/bin") || !contains(profile.Env, "PATH=/offline") || !contains(profile.Env, "TERM") {
		t.Fatalf("Env = %#v, want later profile env to override by key", profile.Env)
	}
}

func TestEffectiveProfileRejectsEmptyCommaProfile(t *testing.T) {
	_, _, _, err := EffectiveProfile(Config{Profiles: map[string]Profile{"default": {}}}, "tool,,offline")
	if err == nil || !strings.Contains(err.Error(), "empty profile") {
		t.Fatalf("error = %v, want empty profile rejection", err)
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
				Inherits: Inherits("tool"),
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
	if !reflect.DeepEqual(got.Profiles["agent"].Inherits.Names, []string{"tool"}) {
		t.Fatalf("Inherits = %#v, want tool", got.Profiles["agent"].Inherits)
	}
	profile := got.Profiles["agent"]
	if !contains(profile.ReadWrite, "state") || !contains(profile.ReadOnly, "prefs") {
		t.Fatalf("profile paths = %#v", profile.PathSettings)
	}
}

func TestBuiltInProfilesLiveInSeparateFiles(t *testing.T) {
	data, err := defaultConfigFS.ReadFile("defaults.toml")
	if err != nil {
		t.Fatalf("ReadFile(defaults.toml) returned error: %v", err)
	}
	if bytes.Contains(data, []byte("[profiles.")) {
		t.Fatalf("defaults.toml still contains profile tables")
	}
	if bytes.Contains(data, []byte("default_profile")) {
		t.Fatalf("defaults.toml still contains default_profile")
	}

	entries, err := defaultConfigFS.ReadDir("profiles")
	if err != nil {
		t.Fatalf("ReadDir(profiles) returned error: %v", err)
	}
	files := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("profiles/%s is a directory, want individual TOML files", entry.Name())
		}
		path := "profiles/" + entry.Name()
		data, err := defaultConfigFS.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s) returned error: %v", path, err)
		}
		if bytes.Contains(data, []byte("[profiles.")) {
			t.Fatalf("%s still contains nested profile tables", path)
		}
		files[entry.Name()] = true
	}
	for _, want := range []string{
		"default.toml",
		"tool.toml",
		"network.toml",
		"offline.toml",
		"macos-dns.toml",
		"macos-certs.toml",
		"keychain.toml",
		"codex.toml",
		"claude.toml",
		"opencode.toml",
		"pi.toml",
	} {
		if !files[want] {
			t.Fatalf("profiles/%s missing in embedded defaults", want)
		}
	}
}

func TestLoadDefaultConfigReadsSingleProfileFileSpec(t *testing.T) {
	cfg, err := loadDefaultConfigFromFS(fstest.MapFS{
		"defaults.toml": {Data: []byte(``)},
		"profiles/tool.toml": {Data: []byte(`title = "Tool"
description = "General command support"
inherits = "network"
env = ["PATH"]

[macos]
ro = ["$HOME/Library/Preferences"]
`)},
	})
	if err != nil {
		t.Fatalf("loadDefaultConfigFromFS returned error: %v", err)
	}
	meta := cfg.ProfileMetadata["tool"]
	if meta.Title != "Tool" || meta.Description != "General command support" {
		t.Fatalf("metadata = %#v", meta)
	}
	profile := cfg.Profiles["tool"]
	if !reflect.DeepEqual(profile.Inherits.Names, []string{"network"}) || !contains(profile.Env, "PATH") || !contains(profile.MacOS.ReadOnly, "$HOME/Library/Preferences") {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestLoadDefaultConfigRejectsRemovedProfileMetadataFields(t *testing.T) {
	for name, data := range map[string]string{
		"category": `category = "base"`,
		"hidden":   `hidden = true`,
		"order":    `order = 10`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadDefaultConfigFromFS(fstest.MapFS{
				"defaults.toml":      {Data: []byte(``)},
				"profiles/tool.toml": {Data: []byte(data)},
			})
			if err == nil || !strings.Contains(err.Error(), "profiles/tool.toml") {
				t.Fatalf("error = %v, want removed metadata field rejection", err)
			}
		})
	}
}

func TestLoadDefaultConfigRejectsNestedProfileFileTables(t *testing.T) {
	_, err := loadDefaultConfigFromFS(fstest.MapFS{
		"defaults.toml": {Data: []byte(``)},
		"profiles/tool.toml": {Data: []byte(`[profiles.tool]
env = ["PATH"]
`)},
	})
	if err == nil || !strings.Contains(err.Error(), "profiles/tool.toml") {
		t.Fatalf("error = %v, want nested profile table rejection", err)
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
	if profile.AddExec == nil || !*profile.AddExec || profile.AddLibs == nil || !*profile.AddLibs {
		t.Fatalf("AddExec = %v, AddLibs = %v", profile.AddExec, profile.AddLibs)
	}
	if !contains(profile.Allow, "network") || contains(profile.Deny, "network") {
		t.Fatalf("capabilities = allow %#v deny %#v, want network allowed", profile.Allow, profile.Deny)
	}
}

func TestDefaultConfigIncludesCapabilityBundles(t *testing.T) {
	cfg := DefaultConfig()

	network, ok := cfg.Profiles["network"]
	if !ok {
		t.Fatalf("network profile missing")
	}
	if !reflect.DeepEqual(network.Inherits.Names, []string{"macos-dns", "macos-certs"}) {
		t.Fatalf("network Inherits = %#v, want macos DNS and cert bundles", network.Inherits)
	}
	if !contains(network.Allow, "network") || contains(network.Deny, "network") {
		t.Fatalf("network capabilities = allow %#v deny %#v, want network allowed", network.Allow, network.Deny)
	}

	offline, ok := cfg.Profiles["offline"]
	if !ok {
		t.Fatalf("offline profile missing")
	}
	for _, want := range []string{
		"com.apple.SystemConfiguration.DNSConfiguration",
		"com.apple.trustd.agent",
	} {
		if !contains(offline.MacOS.DenyMachLookup, want) {
			t.Fatalf("offline DenyMachLookup = %#v, want %q", offline.MacOS.DenyMachLookup, want)
		}
	}

	dns, ok := cfg.Profiles["macos-dns"]
	if !ok {
		t.Fatalf("macos-dns profile missing")
	}
	for _, want := range []string{
		"com.apple.SystemConfiguration.DNSConfiguration",
		"com.apple.SystemConfiguration.configd",
		"com.apple.system.opendirectoryd.libinfo",
	} {
		if !contains(dns.MacOS.MachLookup, want) {
			t.Fatalf("macos-dns MachLookup = %#v, want %q", dns.MacOS.MachLookup, want)
		}
	}

	certs, ok := cfg.Profiles["macos-certs"]
	if !ok {
		t.Fatalf("macos-certs profile missing")
	}
	if !contains(certs.MacOS.MachLookup, "com.apple.trustd.agent") {
		t.Fatalf("macos-certs MachLookup = %#v, want trustd", certs.MacOS.MachLookup)
	}

	keychain, ok := cfg.Profiles["keychain"]
	if !ok {
		t.Fatalf("keychain profile missing")
	}
	for _, want := range []string{
		"$HOME/Library/Keychains",
		"/Library/Keychains",
		"/Library/Security",
	} {
		if !contains(keychain.MacOS.ReadOnly, want) {
			t.Fatalf("keychain ReadOnly = %#v, want %q", keychain.MacOS.ReadOnly, want)
		}
	}
	for _, want := range []string{
		"com.apple.SecurityServer",
		"com.apple.securityd",
		"com.apple.securityd.xpc",
	} {
		if !contains(keychain.MacOS.MachLookup, want) {
			t.Fatalf("keychain MachLookup = %#v, want %q", keychain.MacOS.MachLookup, want)
		}
	}
}

func TestDefaultConfigIncludesAgentProfiles(t *testing.T) {
	cfg := DefaultConfig()
	for name, tt := range map[string]struct {
		app       string
		statePath string
		inherits  []string
	}{
		"codex":    {app: "codex", statePath: "$HOME/.codex", inherits: []string{"tool", "keychain"}},
		"claude":   {app: "claude", statePath: "$HOME/.claude", inherits: []string{"tool"}},
		"opencode": {app: "opencode", statePath: "$HOME/.config/opencode", inherits: []string{"tool"}},
		"pi":       {app: "pi", statePath: "$HOME/.pi", inherits: []string{"tool"}},
	} {
		profile, err := cfg.ResolveProfile(name)
		if err != nil {
			t.Fatalf("ResolveProfile(%q) returned error: %v", name, err)
		}
		if profile.DefaultApp != tt.app {
			t.Fatalf("profile %q DefaultApp = %q, want %q", name, profile.DefaultApp, tt.app)
		}
		if !reflect.DeepEqual(cfg.Profiles[name].Inherits.Names, tt.inherits) {
			t.Fatalf("profile %q Inherits = %#v, want %#v", name, cfg.Profiles[name].Inherits, tt.inherits)
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

func TestCodexProfileSupportsAppMCPOnMacOS(t *testing.T) {
	cfg := DefaultConfig()
	profile, err := cfg.ResolveProfile("codex")
	if err != nil {
		t.Fatalf("ResolveProfile returned error: %v", err)
	}
	assertMacOSPreferencesDefault(t, "codex", profile)
	hasSecurity := contains(profile.MachLookup, "com.apple.SecurityServer")
	hasKeychainRO := contains(profile.ReadOnly, "$HOME/Library/Keychains")
	if runtime.GOOS == "darwin" {
		if !hasSecurity {
			t.Fatalf("codex MachLookup = %#v, want SecurityServer on darwin", profile.MachLookup)
		}
		if !hasKeychainRO {
			t.Fatalf("codex ReadOnly = %#v, want user keychain on darwin", profile.ReadOnly)
		}
	} else {
		if hasSecurity || hasKeychainRO {
			t.Fatalf("codex macOS grants leaked on %s: ro=%#v mach=%#v", runtime.GOOS, profile.ReadOnly, profile.MachLookup)
		}
	}
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
	if contains(profile.MachLookup, "com.apple.SecurityServer") || contains(profile.ReadOnly, "$HOME/Library/Keychains") {
		t.Fatalf("claude keychain grants = ro %#v mach %#v, want none", profile.ReadOnly, profile.MachLookup)
	}
	assertMacOSPreferencesDefault(t, "claude", profile)
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
deny = ["network"]
`)
	cfg, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	top := cfg.TopLevelProfile()
	if !contains(top.ReadWrite, "$WORKSPACE/.venv") || !contains(top.Env, "PYTHONPATH") || !contains(top.Deny, "network") {
		t.Fatalf("top = %#v", top)
	}
}

func TestPlatformSettingsApplyByPlatform(t *testing.T) {
	cfg := DefaultConfig()
	macOS := cfg.Platform.ForPlatform("macos")
	if !contains(macOS.Exec.ReadOnlyExec, "/opt/homebrew/bin") {
		t.Fatalf("macOS exec defaults = %#v", macOS.Exec.ReadOnlyExec)
	}
	if len(macOS.MachLookup) != 0 {
		t.Fatalf("macOS platform MachLookup = %#v, want profile bundles to own Mach services", macOS.MachLookup)
	}
	linux := cfg.Platform.ForPlatform("linux")
	if contains(linux.Exec.ReadOnlyExec, "/opt/homebrew/bin") {
		t.Fatalf("linux exec defaults leaked macOS path: %#v", linux.Exec.ReadOnlyExec)
	}
	if len(linux.MachLookup) != 0 {
		t.Fatalf("linux MachLookup = %#v, want none", linux.MachLookup)
	}
}

func TestPlatformExecAndLibsSettingsArePathOnly(t *testing.T) {
	settingsType := reflect.TypeOf(PlatformSettings{}.Exec)
	for _, field := range []string{
		"DefaultApp",
		"Env",
		"MachLookup",
		"AddExec",
		"AddLibs",
	} {
		if _, ok := settingsType.FieldByName(field); ok {
			t.Fatalf("PlatformSettings.Exec exposes non-path field %s", field)
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
