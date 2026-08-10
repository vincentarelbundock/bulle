package env

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func Resolve(parent map[string]string, env []string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range env {
		key, value, explicit := strings.Cut(item, "=")
		if !explicit && strings.ContainsAny(key, "*?[") {
			// A name glob like GIT_* copies every matching parent variable.
			// Parent names that are not valid variable names (exported shell
			// functions and similar) are skipped rather than erroring.
			if _, err := filepath.Match(key, ""); err != nil {
				return nil, fmt.Errorf("invalid environment variable pattern %q: %w", key, err)
			}
			for _, name := range sortedNames(parent) {
				if ok, _ := filepath.Match(key, name); ok && validateName(name) == nil {
					out[name] = parent[name]
				}
			}
			continue
		}
		if err := validateName(key); err != nil {
			return nil, fmt.Errorf("invalid environment variable %q: %w", key, err)
		}
		if explicit {
			out[key] = value
			continue
		}
		if val, ok := parent[key]; ok {
			out[key] = val
		}
	}
	return out, nil
}

func sortedNames(env map[string]string) []string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	for i, r := range name {
		if r == '_' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' || i > 0 && '0' <= r && r <= '9' {
			continue
		}
		return fmt.Errorf("name must match [A-Za-z_][A-Za-z0-9_]*")
	}
	return nil
}
