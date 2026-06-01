package backends

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ttyDevicePattern = regexp.MustCompile(`^/dev/ttys[0-9]+$`)

func appendPaths(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

func writableFileScratchPatterns(paths []string) []string {
	patterns := []string{}
	for _, path := range paths {
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) || isDirectory(clean) {
			continue
		}
		// Many tools update writable config files by creating sibling lock
		// and temporary files, then renaming them over the original. If a
		// policy allows a file to be written, allow those sidecars for that
		// same file without granting the containing directory.
		patterns = append(patterns, "^"+regexp.QuoteMeta(clean)+`\.(lock|tmp\.[^/]*)$`)
	}
	return patterns
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func splitPathTypes(paths []string) (files []string, dirs []string) {
	for _, path := range paths {
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) {
			continue
		}
		if isDirectory(clean) {
			dirs = append(dirs, clean)
		} else {
			files = append(files, clean)
		}
	}
	return files, dirs
}

func ttyDevicePaths() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, fd := range []int{0, 1, 2} {
		path, ok := fcntlGetPath(fd)
		if !ok {
			continue
		}
		clean := filepath.Clean(path)
		if !ttyDevicePattern.MatchString(clean) || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

func ancestorDirs(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) {
			continue
		}
		var dirs []string
		if clean == string(filepath.Separator) {
			dirs = append(dirs, clean)
		} else {
			for dir := filepath.Dir(clean); ; dir = filepath.Dir(dir) {
				dirs = append(dirs, dir)
				if dir == string(filepath.Separator) {
					break
				}
			}
		}
		for i := len(dirs) - 1; i >= 0; i-- {
			if !seen[dirs[i]] {
				seen[dirs[i]] = true
				out = append(out, dirs[i])
			}
		}
	}
	return out
}

func symlinkPathComponents(paths []string) []string {
	seenOutput := map[string]bool{}
	seenVisits := map[string]bool{}
	out := []string{}

	var visit func(string)
	visit = func(path string) {
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) || seenVisits[clean] {
			return
		}
		seenVisits[clean] = true

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
			if !seenOutput[prefix] {
				seenOutput[prefix] = true
				out = append(out, prefix)
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

	for _, path := range paths {
		visit(path)
	}
	return out
}

func splitRoot(paths []string) ([]string, []string) {
	var root []string
	var nonRoot []string
	for _, path := range paths {
		if path == string(filepath.Separator) {
			root = append(root, path)
		} else {
			nonRoot = append(nonRoot, path)
		}
	}
	return root, nonRoot
}
