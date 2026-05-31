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
	defaults := Policy{}
	usesToolDefaults := profileUsesToolDefaults(profileName, cfg)
	if usesToolDefaults {
		defaults = platformToolPolicy(cfg)
	} else if profile.AddLibs || in.Options.AddLibs {
		defaults = platformLibsPolicy(cfg)
	}
	p := defaults
	p.Backend = backend
	p.ProjectPath = in.Options.ProjectPath
	p.Command = in.Options.Command
	p.AddExec = profile.AddExec || in.Options.AddExec
	p.AddLibs = profile.AddLibs || in.Options.AddLibs
	p.AllowKeychain = boolValue(profile.AllowKeychain)

	p.ReadOnly = append(p.ReadOnly, profile.ReadOnly...)
	p.ReadOnly = append(p.ReadOnly, in.Options.ReadOnly...)
	p.ReadOnlyExec = append(p.ReadOnlyExec, profile.ReadOnlyExec...)
	p.ReadOnlyExec = append(p.ReadOnlyExec, in.Options.ReadOnlyExec...)
	p.ReadWrite = append(p.ReadWrite, profile.ReadWrite...)
	p.ReadWrite = append(p.ReadWrite, in.Options.ReadWrite...)
	p.ReadWriteExec = append(p.ReadWriteExec, profile.ReadWriteExec...)
	p.ReadWriteExec = append(p.ReadWriteExec, in.Options.ReadWriteExec...)

	vars, err := pathVars(p.ProjectPath, in.Home, in.Tmp, cfg.Vars)
	if err != nil {
		return Policy{}, err
	}
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

func pathVars(workspace, home, tmp string, configured map[string]string) (bpaths.Vars, error) {
	vars := bpaths.Vars{"WORKSPACE": workspace, "HOME": home, "TMPDIR": tmp, "TMP": tmp}
	for key, value := range configured {
		if isReservedPathVar(key) {
			return nil, fmt.Errorf("path variable %s is reserved and cannot be overridden", key)
		}
		vars[key] = value
	}
	return vars, nil
}

func isReservedPathVar(key string) bool {
	switch key {
	case "WORKSPACE", "HOME", "TMPDIR", "TMP":
		return true
	default:
		return false
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func profileUsesToolDefaults(name string, cfg config.Config) bool {
	return profileUsesNamedProfile(name, cfg, "tool")
}

func profileUsesNamedProfile(name string, cfg config.Config, want string) bool {
	if name == "" {
		name = cfg.DefaultProfile
	}
	seen := map[string]bool{}
	for name != "" {
		if name == want {
			return true
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		profile, ok := cfg.Profiles[name]
		if !ok {
			return false
		}
		name = profile.Inherits
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
	p := platformLibsPolicy(cfg)
	exec := policyFromPathSettings(cfg.Platform.Exec)
	p.ReadOnly = append(exec.ReadOnly, p.ReadOnly...)
	p.ReadOnlyExec = append(exec.ReadOnlyExec, p.ReadOnlyExec...)
	p.ReadWrite = append(exec.ReadWrite, p.ReadWrite...)
	p.ReadWriteExec = append(exec.ReadWriteExec, p.ReadWriteExec...)
	return p
}

func platformLibsPolicy(cfg config.Config) Policy {
	return policyFromPathSettings(cfg.Platform.Libs)
}

func policyFromPathSettings(s config.PathSettings) Policy {
	return Policy{
		ReadOnly:      append([]string{}, s.ReadOnly...),
		ReadOnlyExec:  append([]string{}, s.ReadOnlyExec...),
		ReadWrite:     append([]string{}, s.ReadWrite...),
		ReadWriteExec: append([]string{}, s.ReadWriteExec...),
	}
}
