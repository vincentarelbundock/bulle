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
