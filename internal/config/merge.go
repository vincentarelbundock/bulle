package config

import "fmt"

func MergeConfigs(parent Config, child Config) Config {
	out := cloneConfig(parent)
	out.Settings = MergeSettings(parent.Settings, child.Settings)
	out.DefaultProfile = choose(child.DefaultProfile, parent.DefaultProfile)
	out.Vars = mergeMap(parent.Vars, child.Vars)
	out.Platform = MergePlatformSettings(parent.Platform, child.Platform)
	if out.Profiles == nil {
		out.Profiles = map[string]Profile{}
	}
	for name, childProfile := range child.Profiles {
		if parentProfile, ok := out.Profiles[name]; ok {
			out.Profiles[name] = MergeProfiles(parentProfile, childProfile)
		} else {
			out.Profiles[name] = cloneProfile(childProfile)
		}
	}
	return out
}

func MergePlatformSettings(parent, child PlatformSettings) PlatformSettings {
	return PlatformSettings{
		Exec: MergePathSettings(parent.Exec, child.Exec),
		Libs: MergePathSettings(parent.Libs, child.Libs),
	}
}

func (c Config) ResolveProfile(name string) (Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	seen := map[string]bool{}
	return c.resolveProfile(name, seen)
}

func (c Config) resolveProfile(name string, seen map[string]bool) (Profile, error) {
	if seen[name] {
		return Profile{}, fmt.Errorf("profile inheritance cycle at %s", name)
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("profile %s not found", name)
	}
	if p.Inherits == "" {
		return p, nil
	}
	seen[name] = true
	parent, err := c.resolveProfile(p.Inherits, seen)
	if err != nil {
		return Profile{}, err
	}
	return MergeProfiles(parent, p), nil
}

func MergeProfiles(parent Profile, child Profile) Profile {
	return Profile{
		Inherits: choose(child.Inherits, parent.Inherits),
		Settings: MergeSettings(parent.Settings, child.Settings),
	}
}

func MergeSettings(parent Settings, child Settings) Settings {
	out := cloneSettings(parent)
	out.DefaultApp = choose(child.DefaultApp, parent.DefaultApp)
	out.PathSettings = MergePathSettings(parent.PathSettings, child.PathSettings)
	out.Env = mergeList(parent.Env, child.Env, child.ReplaceEnv)
	out.AddExec = child.AddExec || parent.AddExec
	out.AddLibs = child.AddLibs || parent.AddLibs
	out.AllowKeychain = mergeBoolPtr(parent.AllowKeychain, child.AllowKeychain)
	return out
}

func MergePathSettings(parent PathSettings, child PathSettings) PathSettings {
	out := clonePathSettings(parent)
	out.ReadOnly = mergeList(parent.ReadOnly, child.ReadOnly, child.ReplaceReadOnly)
	out.ReadOnlyExec = mergeList(parent.ReadOnlyExec, child.ReadOnlyExec, child.ReplaceReadOnlyExec)
	out.ReadWrite = mergeList(parent.ReadWrite, child.ReadWrite, child.ReplaceReadWrite)
	out.ReadWriteExec = mergeList(parent.ReadWriteExec, child.ReadWriteExec, child.ReplaceReadWriteExec)
	return out
}

func mergeList(parent []string, child []string, replace bool) []string {
	if replace {
		return append([]string{}, child...)
	}
	out := append([]string{}, parent...)
	out = append(out, child...)
	return out
}

func choose(child string, parent string) string {
	if child != "" {
		return child
	}
	return parent
}

func mergeMap(parent map[string]string, child map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range parent {
		out[key] = value
	}
	for key, value := range child {
		out[key] = value
	}
	return out
}

func EffectiveProfile(global Config, explicitProfile string) (Profile, string, Config, error) {
	cfg := cloneConfig(global)
	name := explicitProfile
	if name == "" {
		name = cfg.DefaultProfile
	}
	profile, err := cfg.ResolveProfile(name)
	if err != nil {
		return Profile{}, "", Config{}, err
	}
	topLevel := cfg.TopLevelProfile()
	topLevel.DefaultApp = ""
	profile = MergeProfiles(profile, topLevel)
	if profile.DefaultApp == "" {
		profile.DefaultApp = cfg.DefaultApp
	}
	return profile, name, cfg, nil
}

func cloneConfig(c Config) Config {
	return Config{
		Settings:       cloneSettings(c.Settings),
		DefaultProfile: c.DefaultProfile,
		Vars:           mergeMap(c.Vars, nil),
		Profiles:       cloneProfiles(c.Profiles),
		Platform:       clonePlatformSettings(c.Platform),
	}
}

func clonePlatformSettings(p PlatformSettings) PlatformSettings {
	return PlatformSettings{
		Exec: clonePathSettings(p.Exec),
		Libs: clonePathSettings(p.Libs),
	}
}

func cloneProfiles(profiles map[string]Profile) map[string]Profile {
	if profiles == nil {
		return nil
	}
	out := make(map[string]Profile, len(profiles))
	for name, profile := range profiles {
		out[name] = cloneProfile(profile)
	}
	return out
}

func cloneProfile(p Profile) Profile {
	return Profile{
		Inherits: p.Inherits,
		Settings: cloneSettings(p.Settings),
	}
}

func cloneSettings(s Settings) Settings {
	return Settings{
		DefaultApp:    s.DefaultApp,
		PathSettings:  clonePathSettings(s.PathSettings),
		Env:           cloneList(s.Env),
		ReplaceEnv:    s.ReplaceEnv,
		AddExec:       s.AddExec,
		AddLibs:       s.AddLibs,
		AllowKeychain: cloneBoolPtr(s.AllowKeychain),
	}
}

func clonePathSettings(s PathSettings) PathSettings {
	return PathSettings{
		ReadOnly:             cloneList(s.ReadOnly),
		ReadOnlyExec:         cloneList(s.ReadOnlyExec),
		ReadWrite:            cloneList(s.ReadWrite),
		ReadWriteExec:        cloneList(s.ReadWriteExec),
		ReplaceReadOnly:      s.ReplaceReadOnly,
		ReplaceReadOnlyExec:  s.ReplaceReadOnlyExec,
		ReplaceReadWrite:     s.ReplaceReadWrite,
		ReplaceReadWriteExec: s.ReplaceReadWriteExec,
	}
}

func cloneList(xs []string) []string {
	if xs == nil {
		return nil
	}
	return append([]string{}, xs...)
}

func mergeBoolPtr(parent *bool, child *bool) *bool {
	if child != nil {
		return cloneBoolPtr(child)
	}
	return cloneBoolPtr(parent)
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
