package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func MergeConfigs(parent Config, child Config) Config {
	out := cloneConfig(parent)
	out.Settings = MergeSettings(parent.Settings, child.Settings)
	out.Platform = MergePlatformSettings(parent.Platform, child.Platform)
	out.ProfileMetadata = mergeProfileMetadata(parent.ProfileMetadata, child.ProfileMetadata)
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
		Exec:       MergePathSettings(parent.Exec, child.Exec),
		Libs:       MergePathSettings(parent.Libs, child.Libs),
		MachLookup: mergeStringSet(parent.MachLookup, child.MachLookup),
		MacOS:      MergePlatformPathSettings(parent.MacOS, child.MacOS),
		Linux:      MergePlatformPathSettings(parent.Linux, child.Linux),
	}
}

func MergePlatformPathSettings(parent, child PlatformPathSettings) PlatformPathSettings {
	return PlatformPathSettings{
		Exec:       MergePathSettings(parent.Exec, child.Exec),
		Libs:       MergePathSettings(parent.Libs, child.Libs),
		MachLookup: mergeStringSet(parent.MachLookup, child.MachLookup),
	}
}

func (p PlatformSettings) ForCurrentPlatform() PlatformSettings {
	return p.ForPlatform(currentPlatformKey())
}

func (p PlatformSettings) ForPlatform(platform string) PlatformSettings {
	out := PlatformSettings{Exec: clonePathSettings(p.Exec), Libs: clonePathSettings(p.Libs), MachLookup: cloneList(p.MachLookup)}
	switch platform {
	case "macos":
		out.Exec = MergePathSettings(out.Exec, p.MacOS.Exec)
		out.Libs = MergePathSettings(out.Libs, p.MacOS.Libs)
		out.MachLookup = mergeStringSet(out.MachLookup, p.MacOS.MachLookup)
	case "linux":
		out.Exec = MergePathSettings(out.Exec, p.Linux.Exec)
		out.Libs = MergePathSettings(out.Libs, p.Linux.Libs)
		out.MachLookup = mergeStringSet(out.MachLookup, p.Linux.MachLookup)
	}
	return out
}

func (c Config) ResolveProfile(name string) (Profile, error) {
	if name == "" {
		name = "default"
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

	seen[name] = true
	out := Profile{}
	for _, parentName := range p.Inherits.Names {
		parent, err := c.resolveProfile(parentName, seen)
		if err != nil {
			return Profile{}, err
		}
		out = MergeProfiles(out, parent)
	}
	seen[name] = false

	out = MergeProfiles(out, profileBase(p))
	out = MergeProfiles(out, profilePlatform(p, currentPlatformKey()))
	return out, nil
}

func profileBase(p Profile) Profile {
	return Profile{Settings: cloneSettings(p.Settings)}
}

func profilePlatform(p Profile, platform string) Profile {
	switch platform {
	case "macos":
		return Profile{Settings: cloneSettings(p.MacOS)}
	case "linux":
		return Profile{Settings: cloneSettings(p.Linux)}
	default:
		return Profile{}
	}
}

func MergeProfiles(parent Profile, child Profile) Profile {
	inherits := cloneInherits(parent.Inherits)
	if child.Inherits.Set {
		inherits = cloneInherits(child.Inherits)
	}
	return Profile{
		Inherits: inherits,
		Settings: MergeSettings(parent.Settings, child.Settings),
		MacOS:    MergeSettings(parent.MacOS, child.MacOS),
		Linux:    MergeSettings(parent.Linux, child.Linux),
	}
}

func MergeSettings(parent Settings, child Settings) Settings {
	out := cloneSettings(parent)
	out.DefaultApp = choose(child.DefaultApp, parent.DefaultApp)
	out.PathSettings = MergePathSettings(parent.PathSettings, child.PathSettings)
	out.Env = mergeEnv(parent.Env, child.Env)
	out.Allow, out.Deny = mergeCapabilities(parent.Allow, parent.Deny, child.Allow, child.Deny)
	out.MachLookup, out.DenyMachLookup = mergeSupersedingStringLists(parent.MachLookup, parent.DenyMachLookup, child.MachLookup, child.DenyMachLookup)
	out.AddExec = mergeBoolPtr(parent.AddExec, child.AddExec)
	out.AddLibs = mergeBoolPtr(parent.AddLibs, child.AddLibs)
	return out
}

const (
	pathRead  = 1 << 0
	pathExec  = 1 << 1
	pathWrite = 1 << 2

	pathRO  = pathRead
	pathROX = pathRead | pathExec
	pathRW  = pathRead | pathWrite
	pathRWX = pathRead | pathWrite | pathExec
)

func MergePathSettings(parent PathSettings, child PathSettings) PathSettings {
	type entry struct {
		path  string
		right int
	}
	order := []string{}
	entries := map[string]entry{}
	add := func(paths []string, right int) {
		for _, path := range paths {
			key := pathKey(path)
			if key == "" {
				continue
			}
			current, ok := entries[key]
			if !ok {
				order = append(order, key)
				current = entry{path: path}
			}
			current.right |= right
			current.path = path
			entries[key] = current
		}
	}
	add(parent.ReadOnly, pathRO)
	add(parent.ReadOnlyExec, pathROX)
	add(parent.ReadWrite, pathRW)
	add(parent.ReadWriteExec, pathRWX)
	add(child.ReadOnly, pathRO)
	add(child.ReadOnlyExec, pathROX)
	add(child.ReadWrite, pathRW)
	add(child.ReadWriteExec, pathRWX)

	out := PathSettings{}
	for _, key := range order {
		entry := entries[key]
		switch entry.right {
		case pathRO:
			out.ReadOnly = append(out.ReadOnly, entry.path)
		case pathROX:
			out.ReadOnlyExec = append(out.ReadOnlyExec, entry.path)
		case pathRW:
			out.ReadWrite = append(out.ReadWrite, entry.path)
		case pathRWX:
			out.ReadWriteExec = append(out.ReadWriteExec, entry.path)
		}
	}
	return out
}

func pathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func mergeEnv(parent []string, child []string) []string {
	order := []string{}
	values := map[string]string{}
	add := func(items []string) {
		for _, item := range items {
			key := envKey(item)
			if key == "" {
				continue
			}
			if _, ok := values[key]; !ok {
				order = append(order, key)
			}
			values[key] = item
		}
	}
	add(parent)
	add(child)
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, values[key])
	}
	return out
}

func envKey(item string) string {
	key, _, ok := strings.Cut(item, "=")
	if ok {
		return key
	}
	return item
}

func mergeCapabilities(parentAllow, parentDeny, childAllow, childDeny []string) ([]string, []string) {
	return mergeSupersedingStringLists(parentAllow, parentDeny, childAllow, childDeny)
}

func mergeSupersedingStringLists(parentAllow, parentDeny, childAllow, childDeny []string) ([]string, []string) {
	type grant struct {
		name string
		deny bool
	}
	order := []string{}
	grants := map[string]grant{}
	add := func(names []string, deny bool) {
		for _, name := range names {
			key := strings.TrimSpace(name)
			if key == "" {
				continue
			}
			if _, ok := grants[key]; !ok {
				order = append(order, key)
			}
			grants[key] = grant{name: key, deny: deny}
		}
	}
	add(parentAllow, false)
	add(parentDeny, true)
	add(childAllow, false)
	add(childDeny, true)
	allow := []string{}
	deny := []string{}
	for _, key := range order {
		grant := grants[key]
		if grant.deny {
			deny = append(deny, grant.name)
		} else {
			allow = append(allow, grant.name)
		}
	}
	return allow, deny
}

func mergeStringSet(parent []string, child []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range append(append([]string{}, parent...), child...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func choose(child string, parent string) string {
	if child != "" {
		return child
	}
	return parent
}

func EffectiveProfile(global Config, explicitProfile string) (Profile, string, Config, error) {
	cfg := cloneConfig(global)
	names, err := profileNamesFromSpec(explicitProfile)
	if err != nil {
		return Profile{}, "", Config{}, err
	}
	profile := Profile{}
	for _, name := range names {
		resolved, err := cfg.ResolveProfile(name)
		if err != nil {
			return Profile{}, "", Config{}, err
		}
		profile = MergeProfiles(profile, resolved)
	}
	topLevel := cfg.TopLevelProfile()
	topLevel.DefaultApp = ""
	profile = MergeProfiles(profile, topLevel)
	if profile.DefaultApp == "" {
		profile.DefaultApp = cfg.DefaultApp
	}
	return profile, strings.Join(names, ","), cfg, nil
}

func profileNamesFromSpec(spec string) ([]string, error) {
	if spec == "" {
		return []string{"default"}, nil
	}
	parts := strings.Split(spec, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("profile list contains empty profile name")
		}
		names = append(names, name)
	}
	return names, nil
}

func cloneConfig(c Config) Config {
	return Config{
		Settings:        cloneSettings(c.Settings),
		Profiles:        cloneProfiles(c.Profiles),
		ProfileMetadata: cloneProfileMetadata(c.ProfileMetadata),
		Platform:        clonePlatformSettings(c.Platform),
	}
}

func clonePlatformSettings(p PlatformSettings) PlatformSettings {
	return PlatformSettings{
		Exec:       clonePathSettings(p.Exec),
		Libs:       clonePathSettings(p.Libs),
		MachLookup: cloneList(p.MachLookup),
		MacOS:      clonePlatformPathSettings(p.MacOS),
		Linux:      clonePlatformPathSettings(p.Linux),
	}
}

func clonePlatformPathSettings(p PlatformPathSettings) PlatformPathSettings {
	return PlatformPathSettings{Exec: clonePathSettings(p.Exec), Libs: clonePathSettings(p.Libs), MachLookup: cloneList(p.MachLookup)}
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

func mergeProfileMetadata(parent, child map[string]ProfileMetadata) map[string]ProfileMetadata {
	out := cloneProfileMetadata(parent)
	if out == nil {
		out = map[string]ProfileMetadata{}
	}
	for name, metadata := range child {
		out[name] = metadata
	}
	return out
}

func cloneProfileMetadata(metadata map[string]ProfileMetadata) map[string]ProfileMetadata {
	if metadata == nil {
		return nil
	}
	out := make(map[string]ProfileMetadata, len(metadata))
	for name, value := range metadata {
		out[name] = value
	}
	return out
}

func cloneProfile(p Profile) Profile {
	return Profile{
		Inherits: cloneInherits(p.Inherits),
		Settings: cloneSettings(p.Settings),
		MacOS:    cloneSettings(p.MacOS),
		Linux:    cloneSettings(p.Linux),
	}
}

func cloneSettings(s Settings) Settings {
	return Settings{
		DefaultApp:     s.DefaultApp,
		PathSettings:   clonePathSettings(s.PathSettings),
		Env:            cloneList(s.Env),
		Allow:          cloneList(s.Allow),
		Deny:           cloneList(s.Deny),
		MachLookup:     cloneList(s.MachLookup),
		DenyMachLookup: cloneList(s.DenyMachLookup),
		AddExec:        cloneBoolPtr(s.AddExec),
		AddLibs:        cloneBoolPtr(s.AddLibs),
	}
}

func clonePathSettings(s PathSettings) PathSettings {
	return PathSettings{
		ReadOnly:      cloneList(s.ReadOnly),
		ReadOnlyExec:  cloneList(s.ReadOnlyExec),
		ReadWrite:     cloneList(s.ReadWrite),
		ReadWriteExec: cloneList(s.ReadWriteExec),
	}
}

func cloneInherits(inherits InheritList) InheritList {
	return InheritList{Names: cloneList(inherits.Names), Set: inherits.Set}
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
