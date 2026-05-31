package env

import (
	"fmt"
	"strings"
)

func Resolve(parent map[string]string, env []string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range env {
		key, value, explicit := strings.Cut(item, "=")
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
