package paths

import (
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

func IsWithinAnyRootResolvingSymlinks(path string, roots []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	aliasOK := IsWithinAnyRoot(clean, roots)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return aliasOK
	}
	return IsWithinAnyRoot(filepath.Clean(real), roots)
}
