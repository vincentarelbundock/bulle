package policy

import (
	"fmt"
	"os"
	"sort"
	"strings"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

// composeEnvEntries assembles the environment entry list in increasing order
// of explicitness: profile entries, --env-all-except passthrough, --env-file
// contents, then --env flags, so the most explicit source wins on conflict.
// Explicit values in profile entries ("DENO_DIR=$TMP/bulle/tmp/deno") have
// path variables expanded so a profile can point a tool at a sandbox-owned
// directory; flag and env-file entries pass through verbatim, since a user's
// literal value may legitimately contain a dollar sign.
func composeEnvEntries(profileEnv []string, vars bpaths.Vars, parentEnv map[string]string, allExcept []string, envFiles []string, flagEnv []string) ([]string, error) {
	entries := make([]string, 0, len(profileEnv))
	for _, entry := range profileEnv {
		name, value, explicit := strings.Cut(entry, "=")
		if !explicit || !strings.Contains(value, "$") {
			entries = append(entries, entry)
			continue
		}
		expanded, err := bpaths.ExpandValue(value, vars)
		if err != nil {
			return nil, fmt.Errorf("env entry %q: %w", entry, err)
		}
		entries = append(entries, name+"="+expanded)
	}
	if len(allExcept) > 0 {
		entries = append(entries, allEnvExcept(parentEnv, allExcept)...)
	}
	for _, file := range envFiles {
		fromFile, err := readEnvFile(file)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fromFile...)
	}
	return append(entries, flagEnv...), nil
}

// allEnvExcept returns name-copy entries for every parent variable not in the
// exclusion list. Parent names that are not valid variable names are skipped.
func allEnvExcept(parentEnv map[string]string, except []string) []string {
	excluded := map[string]bool{}
	for _, name := range except {
		excluded[strings.TrimSpace(name)] = true
	}
	names := make([]string, 0, len(parentEnv))
	for name := range parentEnv {
		if !excluded[name] && validEnvName(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' || i > 0 && '0' <= r && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// readEnvFile parses a dotenv-style file: NAME=VALUE lines, blank lines and
// #-comments ignored, an optional "export " prefix stripped, and matching
// single or double quotes removed from the value.
func readEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading --env-file: %w", err)
	}
	entries := []string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || !validEnvName(name) {
			return nil, fmt.Errorf("invalid line %d in %s: expected NAME=VALUE", i+1, path)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'' {
				value = value[1 : len(value)-1]
			}
		}
		entries = append(entries, name+"="+value)
	}
	return entries, nil
}
