package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

type shimEntry struct {
	name  string
	alias string
}

// pkgRootDenylist are directories that a pkg: entry must never expand to as
// its package root: granting one would hand over a whole system tree instead
// of a single tool's package. Binaries installed directly in a system bin
// directory should use which: instead.
var pkgRootDenylist = map[string]bool{
	"/":              true,
	"/bin":           true,
	"/sbin":          true,
	"/usr":           true,
	"/usr/bin":       true,
	"/usr/sbin":      true,
	"/usr/local":     true,
	"/usr/local/bin": true,
	"/opt":           true,
	"/opt/homebrew":  true,
	"/etc":           true,
	"/var":           true,
	"/tmp":           true,
	"/home":          true,
	"/Users":         true,
}

// expandExecResolvers replaces which:NAME and pkg:NAME entries in an exec
// grant list with the concrete paths they resolve to on the parent PATH.
// which: grants the binary (and its symlink chain); pkg: additionally grants
// the package tree two levels above the real binary, which Node- and
// Python-style tools need for their libraries. Name findability inside the
// sandbox comes from the shim directory, never from granting the binary's
// containing directory.
func expandExecResolvers(entries []string, list string, parentPATH string, home string) ([]string, []shimEntry, []bpaths.Trace, error) {
	out := make([]string, 0, len(entries))
	shims := []shimEntry{}
	traces := []bpaths.Trace{}
	for _, entry := range entries {
		path, optional, _ := bpaths.ParseMarkers(strings.TrimSpace(entry))
		if !bpaths.IsResolverEntry(path) {
			out = append(out, entry)
			continue
		}
		kind, name, _ := strings.Cut(path, ":")
		trace := bpaths.Trace{Raw: entry, List: list, Source: bpaths.SourceUser}
		if name == "" || strings.ContainsRune(name, filepath.Separator) {
			return nil, nil, nil, fmt.Errorf("invalid %s: entry %q; expected a bare command name", kind, entry)
		}
		binary, err := lookParentPATH(name, parentPATH)
		if err != nil {
			if optional {
				trace.Outcome = "skipped (not found on PATH)"
				traces = append(traces, trace)
				continue
			}
			return nil, nil, nil, fmt.Errorf("%s: entry %q: %q not found on parent PATH (mark it optional with a ? prefix)", kind, entry, name)
		}
		granted := bpaths.SymlinkPathVariants(binary)
		if kind == "pkg" {
			root, err := packageRootFor(binary, home)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("pkg: entry %q: %w", entry, err)
			}
			granted = append(granted, root)
		}
		out = append(out, granted...)
		shims = append(shims, shimEntry{name: name, alias: binary})
		trace.Outcome = "granted"
		trace.Paths = granted
		traces = append(traces, trace)
	}
	return out, shims, traces, nil
}

func lookParentPATH(name string, parentPATH string) (string, error) {
	for _, dir := range filepath.SplitList(parentPATH) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("command %q not found", name)
}

func packageRootFor(binary string, home string) (string, error) {
	real := binary
	if evaluated, err := filepath.EvalSymlinks(binary); err == nil {
		real = filepath.Clean(evaluated)
	}
	root := filepath.Dir(filepath.Dir(real))
	if pkgRootDenylist[root] || sameExistingPath(root, home) {
		return "", fmt.Errorf("package root %q is a system directory; use which: instead of pkg: for binaries installed there", root)
	}
	return root, nil
}

// createShimDir materializes a bulle-owned directory of symlinks to the
// binaries named by which:/pkg: entries, so name lookup works inside the
// sandbox without granting any real bin directory. It lives outside the
// sandbox's writable tmp tree and is granted read+exec only, so the sandboxed
// process cannot repoint a shim mid-run. Shims point at the alias path (not
// the realpath) because version-manager shims dispatch on their invocation
// path; the alias and its symlink chain are both granted.
func createShimDir(tmpRoot string, shims []shimEntry) (string, error) {
	dir, err := os.MkdirTemp(tmpRoot, "bin-")
	if err != nil {
		return "", fmt.Errorf("creating shim directory: %w", err)
	}
	seen := map[string]bool{}
	for _, shim := range shims {
		if seen[shim.name] {
			continue
		}
		seen[shim.name] = true
		if err := os.Symlink(shim.alias, filepath.Join(dir, shim.name)); err != nil {
			os.RemoveAll(dir)
			return "", fmt.Errorf("creating shim for %s: %w", shim.name, err)
		}
	}
	return dir, nil
}

// rejectResolverEntries refuses which:/pkg: entries in non-exec lists, where
// an executable grant makes no sense and would otherwise fail confusingly.
func rejectResolverEntries(entries []string, list string) error {
	for _, entry := range entries {
		path, _, _ := bpaths.ParseMarkers(strings.TrimSpace(entry))
		if bpaths.IsResolverEntry(path) {
			return fmt.Errorf("%s entry %q: which:/pkg: entries are only valid in rox and rwx lists", list, entry)
		}
	}
	return nil
}
