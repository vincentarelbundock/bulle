package paths

import (
	"os"
	"path/filepath"
	"strings"
)

func CleanAbsolute(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		clean := filepath.Clean(path)
		if !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out
}

func IsWithinAnyRoot(path string, roots []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		if root == string(filepath.Separator) || clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// IsWithinAnyRootResolvingSymlinks reports whether path lies under one of the
// roots once symlinks are followed — the question the kernel answers, since a
// grant names an inode rather than a spelling.
//
// Both sides are resolved. Resolving only the path was an asymmetry with real
// consequences: on macOS every temporary directory is /var/... resolving to
// /private/var/..., so a path literally inside a granted root failed the test
// and was dropped from PATH, and the same happens on any Linux layout that
// reaches a granted tree through a symlink. Comparing the resolved path against
// the unresolved roots alone answers a question nobody asked.
//
// A path whose resolved form escapes every root is still rejected, even when
// its alias sits inside one: that is a symlink pointing out of the sandbox, and
// the kernel would refuse it.
func IsWithinAnyRootResolvingSymlinks(path string, roots []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// Nothing to resolve against (the path may not exist yet); the literal
		// spelling is all there is to judge by.
		return IsWithinAnyRoot(clean, roots)
	}
	return IsWithinAnyRoot(filepath.Clean(real), rootSpellings(roots))
}

// rootSpellings returns each root both as written and as resolved, so a root
// reached through a symlink covers the paths that live under its target.
func rootSpellings(roots []string) []string {
	out := make([]string, 0, len(roots)*2)
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || !filepath.IsAbs(path) {
			return
		}
		clean := filepath.Clean(path)
		if !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	for _, root := range roots {
		add(root)
		if real, err := filepath.EvalSymlinks(root); err == nil {
			add(real)
		}
	}
	return out
}

func SymlinkPathVariants(path string) []string {
	seen := map[string]bool{}
	out := []string{}

	var add func(string)
	add = func(path string) {
		clean := filepath.Clean(path)
		if filepath.IsAbs(clean) && !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}

	var visit func(string)
	visit = func(path string) {
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) || seen[clean] {
			return
		}
		add(clean)

		trimmed := strings.TrimPrefix(clean, string(filepath.Separator))
		if trimmed == "" {
			return
		}
		parts := strings.Split(trimmed, string(filepath.Separator))
		prefix := string(filepath.Separator)
		for i, part := range parts {
			prefix = filepath.Join(prefix, part)
			info, err := os.Lstat(prefix)
			if err != nil {
				return
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(prefix)
			if err != nil {
				return
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(prefix), target)
			}
			expanded := filepath.Clean(target)
			if remaining := parts[i+1:]; len(remaining) > 0 {
				expanded = filepath.Join(append([]string{expanded}, remaining...)...)
			}
			visit(expanded)
		}
	}

	visit(path)
	if real, err := filepath.EvalSymlinks(filepath.Clean(path)); err == nil {
		add(real)
	}
	return out
}
