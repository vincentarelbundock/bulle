package policy

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

// wellKnownToolEnvVars are parent environment variables that path entries may
// reference (typically via ${NAME:-fallback}). Values are untrusted: entries
// that are not absolute paths, or that resolve to / or the home directory, are
// ignored so a hostile parent environment cannot widen a grant.
var wellKnownToolEnvVars = []string{
	"CARGO_HOME", "RUSTUP_HOME",
	"GOPATH", "GOBIN", "GOMODCACHE",
	"NVM_DIR", "VOLTA_HOME", "PNPM_HOME", "BUN_INSTALL", "DENO_DIR", "NPM_CONFIG_CACHE",
	"PYENV_ROOT", "UV_CACHE_DIR", "PIPX_HOME",
	"RBENV_ROOT", "NODENV_ROOT", "SDKMAN_DIR", "MISE_DATA_DIR", "ASDF_DATA_DIR",
}

// RecordingVars exposes the same variable table a run resolves paths against,
// for rewriting observed paths back into the variables a profile should use.
// It shares buildVars with Resolve so the two cannot drift: a variable a run
// can expand is one a recording can substitute, and no other.
//
// User-defined [vars] are deliberately absent. They are machine-local by
// definition, so spelling a recorded entry with one would produce a profile
// that resolves nowhere else.
func RecordingVars(workspace, home, tmp string, parentEnv map[string]string) bpaths.Vars {
	vars, err := buildVars(workspace, home, tmp, parentEnv, nil)
	if err != nil {
		return bpaths.Vars{}
	}
	return vars
}

func reservedVarName(name string) bool {
	switch name {
	case "HOME", "WORKSPACE", "TMP", "TMPDIR", "CONFIG", "DATA", "CACHE", "STATE":
		return true
	}
	return strings.HasPrefix(name, "XDG_")
}

// buildVars assembles the variable table used to resolve path entries:
// built-in variables, platform app-directory variables ($CONFIG, $DATA,
// $CACHE, $STATE, and the raw XDG names on Linux), allowlisted tool
// environment variables, and user-defined variables from [vars] and --var.
func buildVars(workspace, home, tmp string, parentEnv map[string]string, userVars map[string]string) (bpaths.Vars, error) {
	vars := bpaths.Vars{"WORKSPACE": workspace, "HOME": home, "TMPDIR": tmp, "TMP": tmp}
	addPlatformDirVars(vars, home, parentEnv, runtime.GOOS)
	for _, name := range wellKnownToolEnvVars {
		if value, err := safeVarPath(parentEnv[name], home); err == nil {
			vars[name] = value
		}
	}
	for name, value := range userVars {
		if err := validateVarName(name); err != nil {
			return nil, err
		}
		resolved, err := safeVarPath(expandUserVarHome(value, home), home)
		if err != nil {
			return nil, fmt.Errorf("variable %s: %w", name, err)
		}
		vars[name] = resolved
	}
	return vars, nil
}

func addPlatformDirVars(vars bpaths.Vars, home string, parentEnv map[string]string, goos string) {
	if goos == "darwin" {
		appSupport := filepath.Join(home, "Library", "Application Support")
		vars["CONFIG"] = appSupport
		vars["DATA"] = appSupport
		vars["STATE"] = appSupport
		vars["CACHE"] = filepath.Join(home, "Library", "Caches")
		return
	}
	xdg := func(name string, fallback ...string) string {
		if value, err := safeVarPath(parentEnv[name], home); err == nil {
			return value
		}
		return filepath.Join(append([]string{home}, fallback...)...)
	}
	vars["CONFIG"] = xdg("XDG_CONFIG_HOME", ".config")
	vars["DATA"] = xdg("XDG_DATA_HOME", ".local", "share")
	vars["CACHE"] = xdg("XDG_CACHE_HOME", ".cache")
	vars["STATE"] = xdg("XDG_STATE_HOME", ".local", "state")
	vars["XDG_CONFIG_HOME"] = vars["CONFIG"]
	vars["XDG_DATA_HOME"] = vars["DATA"]
	vars["XDG_CACHE_HOME"] = vars["CACHE"]
	vars["XDG_STATE_HOME"] = vars["STATE"]
}

func validateVarName(name string) error {
	if name == "" {
		return fmt.Errorf("variable name is empty")
	}
	if reservedVarName(name) {
		return fmt.Errorf("variable name %s is reserved", name)
	}
	for _, r := range name {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return fmt.Errorf("invalid variable name %q; use uppercase letters, digits, and underscores", name)
	}
	return nil
}

func expandUserVarHome(value string, home string) string {
	if strings.HasPrefix(value, "~/") {
		return filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	return value
}

// safeVarPath validates an untrusted candidate value for a path variable. It
// must be an absolute path and must not name the filesystem root or the home
// directory, so a variable can never silently widen a grant to either.
func safeVarPath(value string, home string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("value is empty")
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("value %q is not an absolute path", value)
	}
	clean := filepath.Clean(value)
	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("value resolves to the filesystem root")
	}
	if home != "" {
		homeReal := filepath.Clean(home)
		if evaluated, err := filepath.EvalSymlinks(home); err == nil {
			homeReal = filepath.Clean(evaluated)
		}
		candidate := clean
		if evaluated, err := filepath.EvalSymlinks(clean); err == nil {
			candidate = filepath.Clean(evaluated)
		}
		if candidate == homeReal || sameExistingPath(clean, home) {
			return "", fmt.Errorf("value resolves to the home directory")
		}
	}
	return clean, nil
}
