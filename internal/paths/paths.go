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
	Path   string
	Source Source
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
			if input.Source == SourceBuiltIn {
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
		return []string{alias, real}, true, nil
	}
	return []string{real}, true, nil
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
