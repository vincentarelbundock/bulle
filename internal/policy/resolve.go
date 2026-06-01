package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	benv "github.com/vincentarelbundock/bulle/internal/env"
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

type Inputs struct {
	Options   cli.Options
	Global    config.Config
	ParentEnv map[string]string
	Home      string
	Tmp       string
}

func Resolve(in Inputs) (Policy, error) {
	backend := RuntimeDefaultBackend()
	profile, profileName, cfg, err := config.EffectiveProfile(in.Global, in.Options.Profile)
	if err != nil {
		return Policy{}, err
	}
	addExec := boolValue(profile.AddExec) || in.Options.AddExec
	addLibs := boolValue(profile.AddLibs) || in.Options.AddLibs
	platformDefaults := cfg.Platform.ForCurrentPlatform()
	defaults := Policy{}
	usesToolDefaults := profileUsesToolDefaults(profileName, cfg)
	if usesToolDefaults {
		defaults = platformToolPolicy(cfg)
	} else if addLibs {
		defaults = platformLibsPolicy(cfg)
	}
	p := defaults
	p.Backend = backend
	p.ProjectPath = in.Options.ProjectPath
	p.Command = in.Options.Command
	p.AddExec = addExec
	p.AddLibs = addLibs
	p.MachLookup = append(p.MachLookup, platformDefaults.MachLookup...)
	p.MachLookup = append(p.MachLookup, profile.MachLookup...)
	p.MachLookup = removeDeniedMachLookups(p.MachLookup, profile.DenyMachLookup)
	p.Network, err = resolveNetworkCapability(profile.Allow, profile.Deny)
	if err != nil {
		return Policy{}, err
	}

	p.ReadOnly = append(p.ReadOnly, profile.ReadOnly...)
	p.ReadOnly = append(p.ReadOnly, in.Options.ReadOnly...)
	p.ReadOnlyExec = append(p.ReadOnlyExec, profile.ReadOnlyExec...)
	p.ReadOnlyExec = append(p.ReadOnlyExec, in.Options.ReadOnlyExec...)
	p.ReadWrite = append(p.ReadWrite, profile.ReadWrite...)
	p.ReadWrite = append(p.ReadWrite, in.Options.ReadWrite...)
	p.ReadWriteExec = append(p.ReadWriteExec, profile.ReadWriteExec...)
	p.ReadWriteExec = append(p.ReadWriteExec, in.Options.ReadWriteExec...)

	vars := pathVars(p.ProjectPath, in.Home, in.Tmp)
	p.ProjectPath, err = resolveProjectPath(p.ProjectPath, vars)
	if err != nil {
		return Policy{}, err
	}
	vars["WORKSPACE"] = p.ProjectPath
	if err := validateProjectPath(p.ProjectPath, in.Home); err != nil {
		return Policy{}, err
	}
	p.ReadOnly, err = resolvePathList(p.ReadOnly, len(defaults.ReadOnly), vars, true)
	if err != nil {
		return Policy{}, err
	}
	p.ReadOnlyExec, err = resolvePathList(p.ReadOnlyExec, len(defaults.ReadOnlyExec), vars, true)
	if err != nil {
		return Policy{}, err
	}
	if !in.Options.NoWorkspace {
		p.ReadWrite = append(p.ReadWrite, "$WORKSPACE")
	}
	p.ReadWrite, err = resolvePathList(p.ReadWrite, len(defaults.ReadWrite), vars, false)
	if err != nil {
		return Policy{}, err
	}
	p.ReadWriteExec, err = resolvePathList(p.ReadWriteExec, len(defaults.ReadWriteExec), vars, false)
	if err != nil {
		return Policy{}, err
	}
	p.Env, err = benv.Resolve(in.ParentEnv, append(profile.Env, in.Options.Env...))
	if err != nil {
		return Policy{}, err
	}
	execRoots := append(append([]string{}, p.ReadOnlyExec...), p.ReadWriteExec...)
	if usesToolDefaults {
		addToolTempEnv(p.Env, sandboxTmpDir(in.Tmp))
	}
	if !(p.AddExec && len(p.Command) > 0) {
		sanitizePolicyPATH(p.Env, execRoots)
	}
	return p, nil
}

func removeDeniedMachLookups(values []string, denied []string) []string {
	if len(values) == 0 || len(denied) == 0 {
		return values
	}
	blocked := map[string]bool{}
	for _, value := range denied {
		name := strings.TrimSpace(value)
		if name != "" {
			blocked[name] = true
		}
	}
	out := values[:0]
	for _, value := range values {
		if !blocked[strings.TrimSpace(value)] {
			out = append(out, value)
		}
	}
	return out
}

func addToolTempEnv(env map[string]string, tmp string) {
	for _, key := range []string{"BUN_TMPDIR", "TMPDIR", "TMP", "TEMP"} {
		if _, ok := env[key]; !ok {
			env[key] = tmp
		}
	}
}

func sandboxTmpDir(tmp string) string {
	path := filepath.Join(tmp, "bulle", "tmp")
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

func pathVars(workspace, home, tmp string) bpaths.Vars {
	return bpaths.Vars{"WORKSPACE": workspace, "HOME": home, "TMPDIR": tmp, "TMP": tmp}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

const CapabilityNetwork = "network"

func resolveNetworkCapability(allow []string, deny []string) (NetworkMode, error) {
	if err := validateCapabilities(allow, deny); err != nil {
		return "", err
	}
	if containsCapability(deny, CapabilityNetwork) {
		return NetworkNone, nil
	}
	if containsCapability(allow, CapabilityNetwork) {
		return NetworkFull, nil
	}
	return NetworkNone, nil
}

func validateCapabilities(lists ...[]string) error {
	for _, list := range lists {
		for _, name := range list {
			switch strings.TrimSpace(name) {
			case "", CapabilityNetwork:
				continue
			default:
				return fmt.Errorf("unsupported capability %q", name)
			}
		}
	}
	return nil
}

func containsCapability(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func profileUsesToolDefaults(name string, cfg config.Config) bool {
	return profileUsesNamedProfile(name, cfg, "tool")
}

func profileUsesNamedProfile(name string, cfg config.Config, want string) bool {
	if name == "" {
		name = "default"
	}
	for _, selected := range strings.Split(name, ",") {
		if profileUsesNamedProfileRecursive(strings.TrimSpace(selected), cfg, want, map[string]bool{}) {
			return true
		}
	}
	return false
}

func profileUsesNamedProfileRecursive(name string, cfg config.Config, want string, seen map[string]bool) bool {
	if name == want {
		return true
	}
	if seen[name] {
		return false
	}
	seen[name] = true
	defer delete(seen, name)
	profile, ok := cfg.Profiles[name]
	if !ok {
		return false
	}
	for _, parentName := range profile.Inherits.Names {
		if profileUsesNamedProfileRecursive(parentName, cfg, want, seen) {
			return true
		}
	}
	return false
}

func resolveProjectPath(path string, vars bpaths.Vars) (string, error) {
	got, exists, err := bpaths.ResolveOne(path, vars)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("workspace path does not exist: %s", path)
	}
	return got, nil
}

func validateProjectPath(project string, home string) error {
	if project == string(filepath.Separator) {
		return fmt.Errorf("refusing to sandbox /")
	}
	if sameExistingPath(project, home) {
		return fmt.Errorf("refusing to sandbox home directory: %s", project)
	}
	return nil
}

func sameExistingPath(a, b string) bool {
	ainfo, err := os.Stat(a)
	if err != nil {
		return false
	}
	binfo, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ainfo, binfo)
}

func resolvePathList(paths []string, builtinCount int, vars bpaths.Vars, optionalMissing bool) ([]string, error) {
	return bpaths.ResolveList(toInputs(paths, builtinCount, optionalMissing), vars)
}

func toInputs(xs []string, builtinCount int, optionalMissing bool) []bpaths.Input {
	out := make([]bpaths.Input, 0, len(xs))
	for i, x := range xs {
		source := bpaths.SourceUser
		if i < builtinCount {
			source = bpaths.SourceBuiltIn
		}
		out = append(out, bpaths.Input{Path: x, Source: source, Optional: optionalMissing})
	}
	return out
}

func sanitizePolicyPATH(env map[string]string, execRoots []string) {
	if path, ok := env["PATH"]; ok {
		env["PATH"] = sanitizePATH(path, execRoots)
	}
}

func sanitizePATH(value string, execRoots []string) string {
	roots := bpaths.CleanAbsolute(execRoots)
	seen := map[string]bool{}
	out := []string{}
	for _, item := range filepath.SplitList(value) {
		if item == "" || !filepath.IsAbs(item) {
			continue
		}
		path := filepath.Clean(item)
		if seen[path] {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			continue
		}
		if bpaths.IsWithinAnyRootResolvingSymlinks(path, roots) {
			seen[path] = true
			out = append(out, path)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func platformToolPolicy(cfg config.Config) Policy {
	platform := cfg.Platform.ForCurrentPlatform()
	p := policyFromPathSettings(platform.Libs)
	exec := policyFromPathSettings(platform.Exec)
	p.ReadOnly = append(exec.ReadOnly, p.ReadOnly...)
	p.ReadOnlyExec = append(exec.ReadOnlyExec, p.ReadOnlyExec...)
	p.ReadWrite = append(exec.ReadWrite, p.ReadWrite...)
	p.ReadWriteExec = append(exec.ReadWriteExec, p.ReadWriteExec...)
	return p
}

func platformLibsPolicy(cfg config.Config) Policy {
	platform := cfg.Platform.ForCurrentPlatform()
	return policyFromPathSettings(platform.Libs)
}

func policyFromPathSettings(s config.PathSettings) Policy {
	return Policy{
		ReadOnly:      append([]string{}, s.ReadOnly...),
		ReadOnlyExec:  append([]string{}, s.ReadOnlyExec...),
		ReadWrite:     append([]string{}, s.ReadWrite...),
		ReadWriteExec: append([]string{}, s.ReadWriteExec...),
	}
}
