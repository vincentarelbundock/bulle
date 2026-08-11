package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Source string

const (
	SourceBuiltIn Source = "builtin"
	SourceUser    Source = "user"
)

const (
	CreateNone = ""
	CreateDir  = "dir"
	CreateFile = "file"
)

type Input struct {
	Path     string
	Source   Source
	Optional bool
	Create   string
}

type Vars map[string]string

// Trace records the outcome of resolving one configured entry, for the
// resolution table shown by --policy.
type Trace struct {
	Raw     string
	List    string
	Source  Source
	Outcome string
	Paths   []string
}

// ParseMarkers strips the optional (?) and create (+) markers from a raw
// configured entry. A + entry with a trailing slash creates a directory when
// missing; without one it creates an empty file.
func ParseMarkers(raw string) (path string, optional bool, create string) {
	path = raw
	for {
		if rest, ok := strings.CutPrefix(path, "?"); ok {
			optional = true
			path = rest
			continue
		}
		if rest, ok := strings.CutPrefix(path, "+"); ok {
			create = CreateFile
			path = rest
			continue
		}
		break
	}
	if create != CreateNone && strings.HasSuffix(path, "/") {
		create = CreateDir
	}
	return path, optional, create
}

// CanonicalEntryKey returns the key under which two spellings of the same
// configured entry should merge: markers are stripped and literal paths are
// cleaned, so "?~/.x", "+~/.x/", and "~/.x" all merge to one grant.
func CanonicalEntryKey(raw string) string {
	path, _, _ := ParseMarkers(strings.TrimSpace(raw))
	if path == "" {
		return ""
	}
	if IsResolverEntry(path) {
		return path
	}
	return filepath.Clean(path)
}

// IsResolverEntry reports whether a configured entry names a resolver rather
// than a literal path. Both spellings are covered: the executable resolvers
// (which:NAME, pkg:NAME), where the caller supplies the right-hand side, and
// the tool resolvers (r:libs, uv:cache), where it names one of a fixed set of
// directories the tool knows about.
func IsResolverEntry(path string) bool {
	_, _, ok := ResolverNamespace(path)
	return ok
}

// ResolverNamespace splits a resolver entry into its namespace and argument.
//
// A leading lowercase word followed by a colon is always a resolver, never a
// path. Colons are legal in filenames, so a literal path in that shape must be
// written with a leading "./" or as an absolute path; that is also why the
// prefix is only recognized at the start of the entry, before any separator.
// Callers must reject unknown namespaces rather than falling back to treating
// the entry as a path: a silently-not-granted path surfaces much later as an
// unexplained denial.
func ResolverNamespace(path string) (namespace string, argument string, ok bool) {
	namespace, argument, found := strings.Cut(path, ":")
	if !found || namespace == "" {
		return "", "", false
	}
	for i, r := range namespace {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && (r >= '0' && r <= '9' || r == '-'):
		default:
			return "", "", false
		}
	}
	return namespace, argument, true
}

func ResolveList(inputs []Input, vars Vars) ([]string, error) {
	out, _, err := ResolveListTrace(inputs, vars)
	return out, err
}

func ResolveListTrace(inputs []Input, vars Vars) ([]string, []Trace, error) {
	out := []string{}
	traces := []Trace{}
	seen := map[string]bool{}
	for _, input := range inputs {
		path, optional, create := ParseMarkers(input.Path)
		optional = optional || input.Optional
		if create == CreateNone {
			create = input.Create
		}
		trace := Trace{Raw: input.Path, Source: input.Source}
		resolved, outcome, err := resolveEntry(path, vars, optional, create, input.Source)
		if err != nil {
			return nil, nil, err
		}
		trace.Outcome = outcome
		trace.Paths = resolved
		traces = append(traces, trace)
		for _, path := range resolved {
			if !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
		}
	}
	return out, traces, nil
}

// resolveEntry resolves one marker-stripped entry, applying glob expansion,
// optional skipping, and creation of missing paths. It returns the granted
// paths and a human-readable outcome for the resolution table.
func resolveEntry(path string, vars Vars, optional bool, create string, source Source) ([]string, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", fmt.Errorf("configured path is empty")
	}
	expanded, err := expand(path, vars)
	if err != nil {
		return nil, "", err
	}
	if strings.Contains(expanded, "*") {
		return resolveGlob(expanded, vars)
	}
	resolved, exists, err := resolve(expanded, vars)
	if err != nil {
		return nil, "", err
	}
	if !exists {
		if create != CreateNone {
			kind, err := createMissing(resolved[len(resolved)-1], create)
			if err != nil {
				return nil, "", err
			}
			resolved, _, err = resolve(expanded, vars)
			if err != nil {
				return nil, "", err
			}
			return resolved, "created (" + kind + ")", nil
		}
		if optional || source == SourceBuiltIn {
			return nil, "skipped (does not exist)", nil
		}
		return nil, "", fmt.Errorf("configured path does not exist: %s (mark it optional with a ? prefix or create it with a + prefix)", path)
	}
	return resolved, "granted", nil
}

func resolveGlob(pattern string, vars Vars) ([]string, string, error) {
	if strings.Contains(pattern, "**") {
		return nil, "", fmt.Errorf("configured path %q uses **; only single * wildcards are supported", pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, "", fmt.Errorf("configured path %q is not a valid glob: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, "skipped (no matches)", nil
	}
	sort.Strings(matches)
	out := []string{}
	for _, match := range matches {
		resolved, exists, err := resolve(match, vars)
		if err != nil {
			return nil, "", err
		}
		if !exists {
			continue
		}
		out = append(out, resolved...)
	}
	if len(out) == 0 {
		return nil, "skipped (no matches)", nil
	}
	return out, fmt.Sprintf("granted (%d matches)", len(matches)), nil
}

func createMissing(path string, create string) (string, error) {
	switch create {
	case CreateDir:
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", fmt.Errorf("creating configured directory %s: %w", path, err)
		}
		return "dir", nil
	case CreateFile:
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", fmt.Errorf("creating parent directory for configured file %s: %w", path, err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("creating configured file %s: %w", path, err)
		}
		file.Close()
		return "file", nil
	default:
		return "", fmt.Errorf("unknown create mode %q", create)
	}
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
		// Compare by filesystem identity, not lexical path: $HOME itself may be a
		// symlink, so a configured grant resolving to the same physical directory
		// must still be refused even though the cleaned strings differ.
		homeReal := filepath.Clean(home)
		if evaluated, err := filepath.EvalSymlinks(home); err == nil {
			homeReal = filepath.Clean(evaluated)
		}
		if real == homeReal {
			return fmt.Errorf("configured path %q resolves through a symlink to the home directory %q; refusing to grant", alias, real)
		}
		if sameFile(real, homeReal) {
			return fmt.Errorf("configured path %q resolves through a symlink to the home directory %q; refusing to grant", alias, homeReal)
		}
	}
	return nil
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// ExpandValue substitutes path variables in a non-path string, for profile
// environment values such as "DENO_DIR=$TMP/bulle/tmp/deno". Unlike path
// resolution it performs no cleaning, globbing, or symlink work.
func ExpandValue(raw string, vars Vars) (string, error) {
	return expand(raw, vars)
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
		name, fallback, hasFallback := strings.Cut(key, ":-")
		if value, ok := vars[name]; ok && value != "" {
			return value
		}
		if hasFallback {
			if home, ok := vars["HOME"]; ok && strings.HasPrefix(fallback, "~/") {
				return filepath.Join(home, strings.TrimPrefix(fallback, "~/"))
			}
			return fallback
		}
		unknown[name] = true
		return ""
	})
	if len(unknown) > 0 {
		for key := range unknown {
			return "", fmt.Errorf("unknown path variable: $%s", key)
		}
	}
	return expanded, nil
}
