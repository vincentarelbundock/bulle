package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Source string

const (
	SourceBuiltIn Source = "builtin"
	SourceUser    Source = "user"
)

type Input struct {
	Path     string
	Source   Source
	Optional bool
}

type Vars map[string]string

func ResolveList(inputs []Input, vars Vars) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, input := range inputs {
		resolved, exists, err := resolve(input.Path, vars)
		if err != nil {
			return nil, err
		}
		if !exists {
			if input.Optional || input.Source == SourceBuiltIn {
				continue
			}
			return nil, fmt.Errorf("configured path does not exist: %s", input.Path)
		}
		for _, path := range resolved {
			if !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
		}
	}
	return out, nil
}

func ResolveOne(raw string, vars Vars) (string, bool, error) {
	resolved, exists, err := resolve(raw, vars)
	if err != nil || len(resolved) == 0 {
		return "", exists, err
	}
	return resolved[len(resolved)-1], exists, nil
}

func resolve(raw string, vars Vars) ([]string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, false, fmt.Errorf("configured path is empty")
	}
	expanded, err := expand(raw, vars)
	if err != nil {
		return nil, false, err
	}
	if !filepath.IsAbs(expanded) {
		abs, err := filepath.Abs(expanded)
		if err != nil {
			return nil, false, err
		}
		expanded = abs
	}
	alias := filepath.Clean(expanded)
	if _, err := os.Stat(expanded); err != nil {
		if os.IsNotExist(err) {
			return []string{alias}, false, nil
		}
		return nil, false, err
	}
	real := alias
	if evaluated, err := filepath.EvalSymlinks(expanded); err == nil {
		real = filepath.Clean(evaluated)
	}
	if real != alias {
		// A configured path is a symlink (or has a symlinked component). We grant
		// the resolved target as well as the alias, which means an attacker who
		// can write inside a granted directory could, between runs, repoint a
		// grant at a sensitive location and have it granted on the next run.
		// Refuse the catastrophic cases outright: a target that resolves to the
		// filesystem root or the home directory would hand over far more than any
		// legitimate grant intends.
		if err := refuseSensitiveSymlinkTarget(alias, real, vars); err != nil {
			return nil, false, err
		}
		return []string{alias, real}, true, nil
	}
	return []string{real}, true, nil
}

func refuseSensitiveSymlinkTarget(alias, real string, vars Vars) error {
	if real == string(filepath.Separator) {
		return fmt.Errorf("configured path %q resolves through a symlink to the filesystem root %q; refusing to grant", alias, real)
	}
	if home, ok := vars["HOME"]; ok && home != "" {
		if real == filepath.Clean(home) {
			return fmt.Errorf("configured path %q resolves through a symlink to the home directory %q; refusing to grant", alias, real)
		}
	}
	return nil
}

func expand(raw string, vars Vars) (string, error) {
	if strings.HasPrefix(raw, "~/") {
		home, ok := vars["HOME"]
		if !ok {
			return "", fmt.Errorf("unknown path variable: $HOME")
		}
		raw = filepath.Join(home, strings.TrimPrefix(raw, "~/"))
	}
	unknown := map[string]bool{}
	expanded := os.Expand(raw, func(key string) string {
		value, ok := vars[key]
		if !ok {
			unknown[key] = true
			return ""
		}
		return value
	})
	if len(unknown) > 0 {
		for key := range unknown {
			return "", fmt.Errorf("unknown path variable: $%s", key)
		}
	}
	return expanded, nil
}
