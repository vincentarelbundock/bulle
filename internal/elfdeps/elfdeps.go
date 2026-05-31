// Package elfdeps contains Linux ELF dependency discovery code derived from Landrun.
//
// Portions of this file are derived from Landrun:
// Copyright (c) 2025 Armin ranjbar
// Used under the MIT License. See LICENSES/landrun-MIT.txt.
package elfdeps

import (
	"debug/elf"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/paths"
)

var standardLibraryDirs = []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/usr/local/lib"}

type DependencyOptions struct {
	TrustedRpathRoots []string
}

func getLdmap() map[string]string {
	m := map[string]string{}
	ldconfig := findTrustedLdconfig()
	if ldconfig == "" {
		return m
	}
	cmd := exec.Command(ldconfig, "-p")
	cmd.Env = []string{"PATH=/usr/sbin:/sbin:/usr/bin:/bin", "LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return m
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "=>") {
			continue
		}
		parts := strings.Split(line, "=>")
		if len(parts) < 2 {
			continue
		}
		path := strings.TrimSpace(parts[len(parts)-1])
		left := strings.TrimSpace(parts[0])
		toks := strings.Fields(left)
		if len(toks) == 0 {
			continue
		}
		soname := toks[0]
		if path == "" || soname == "" {
			continue
		}
		if path, ok := cleanELFDependencyPath(path); ok {
			if _, exists := m[soname]; !exists {
				m[soname] = path
			}
		}
	}
	return m
}

func findTrustedLdconfig() string {
	for _, path := range []string{"/sbin/ldconfig", "/usr/sbin/ldconfig"} {
		if isExecutable(path) {
			return path
		}
	}
	return ""
}

func parseInterp(f *elf.File) string {
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			r := prog.Open()
			if r == nil {
				return ""
			}
			if data, err := io.ReadAll(r); err == nil {
				return strings.TrimRight(string(data), "\x00")
			}
		}
	}
	return ""
}

func parseDynamic(f *elf.File) (needed []string, rpaths []string) {
	needed = []string{}
	rpaths = []string{}

	if libs, err := f.DynString(elf.DT_NEEDED); err == nil {
		needed = append(needed, libs...)
	}
	if rp, err := f.DynString(elf.DT_RPATH); err == nil {
		for _, v := range rp {
			if v != "" {
				rpaths = append(rpaths, strings.Split(v, ":")...)
			}
		}
	}
	if rp, err := f.DynString(elf.DT_RUNPATH); err == nil {
		for _, v := range rp {
			if v != "" {
				rpaths = append(rpaths, strings.Split(v, ":")...)
			}
		}
	}
	return
}

func normalizeRpaths(rpaths []string, origin string) []string {
	out := []string{}
	for _, rp := range rpaths {
		if rp == "" {
			continue
		}
		rp = strings.ReplaceAll(rp, "$ORIGIN", origin)
		rp = strings.ReplaceAll(rp, "${ORIGIN}", origin)
		if !filepath.IsAbs(rp) {
			rp = filepath.Join(origin, rp)
		}
		out = append(out, rp)
	}
	return out
}

func resolveSingleSoname(soname string, rpaths []string, stdDirs []string, ldmap *map[string]string) string {
	if soname == "" || strings.Contains(soname, string(filepath.Separator)) {
		return ""
	}
	for _, rp := range rpaths {
		candidate := filepath.Join(rp, soname)
		if path, ok := cleanELFDependencyPath(candidate); ok {
			return path
		}
	}
	for _, d := range stdDirs {
		candidate := filepath.Join(d, soname)
		if path, ok := cleanELFDependencyPath(candidate); ok {
			return path
		}
	}
	if *ldmap == nil {
		*ldmap = getLdmap()
	}
	if p, ok := (*ldmap)[soname]; ok {
		if path, ok := cleanELFDependencyPath(p); ok {
			return path
		}
	}
	return ""
}

func resolveSonames(needed []string, rpaths []string) []string {
	resolved := map[string]string{}
	var ldmap map[string]string

	for _, soname := range needed {
		if _, ok := resolved[soname]; ok {
			continue
		}
		resolved[soname] = resolveSingleSoname(soname, rpaths, standardLibraryDirs, &ldmap)
	}
	out := []string{}
	for _, r := range resolved {
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func GetSystemLibraryDependencies(binary string) ([]string, error) {
	return GetLibraryDependencies(binary, DependencyOptions{})
}

func GetLibraryDependencies(binary string, opts DependencyOptions) ([]string, error) {
	queue := []string{binary}
	processed := map[string]struct{}{}
	finalMap := map[string]struct{}{}

	if _, err := os.Stat("/etc/ld.so.cache"); err == nil {
		finalMap["/etc/ld.so.cache"] = struct{}{}
	}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if _, ok := processed[curr]; ok {
			continue
		}
		processed[curr] = struct{}{}

		info, ok := readDependencyInfo(curr, curr == binary)
		if !ok {
			continue
		}

		if curr == binary {
			if info.interp != "" {
				if path, ok := cleanELFDependencyPath(info.interp); ok && runtimePathAllowed(path, opts.TrustedRpathRoots) {
					finalMap[path] = struct{}{}
					queue = append(queue, path)
				}
			}
		}

		origin := filepath.Dir(curr)
		rpaths := normalizeRpaths(info.rpaths, origin)
		rpaths = filterRpathsByRoots(rpaths, opts.TrustedRpathRoots)
		libPaths := resolveSonames(info.needed, rpaths)

		for _, p := range libPaths {
			if _, ok := finalMap[p]; !ok {
				finalMap[p] = struct{}{}
				queue = append(queue, p)
			}
		}
	}

	out := make([]string, 0, len(finalMap))
	for p := range finalMap {
		out = append(out, p)
	}
	return out, nil
}

type dependencyInfo struct {
	interp string
	needed []string
	rpaths []string
}

func readDependencyInfo(path string, includeInterp bool) (dependencyInfo, bool) {
	f, err := elf.Open(path)
	if err != nil {
		return dependencyInfo{}, false
	}
	defer f.Close()

	info := dependencyInfo{}
	if includeInterp {
		info.interp = parseInterp(f)
	}
	info.needed, info.rpaths = parseDynamic(f)
	return info, true
}

func cleanELFDependencyPath(path string) (string, bool) {
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		clean = filepath.Clean(real)
	}
	if !isELFDependency(clean) {
		return "", false
	}
	return clean, true
}

func isELFDependency(path string) bool {
	f, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return f.Type == elf.ET_DYN
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func filterRpathsByRoots(rpaths []string, roots []string) []string {
	cleanRoots := cleanRootAliasesAndRealpaths(roots)
	if len(cleanRoots) == 0 {
		return nil
	}
	out := []string{}
	for _, rpath := range rpaths {
		if rpath == "" || !filepath.IsAbs(rpath) {
			continue
		}
		clean := filepath.Clean(rpath)
		real, err := filepath.EvalSymlinks(clean)
		if err != nil {
			continue
		}
		real = filepath.Clean(real)
		if paths.IsWithinAnyRoot(real, cleanRoots) {
			out = append(out, real)
		}
	}
	return out
}

func cleanRootAliasesAndRealpaths(roots []string) []string {
	cleanRoots := paths.CleanAbsolute(roots)
	out := make([]string, 0, len(cleanRoots)*2)
	seen := map[string]bool{}
	for _, root := range cleanRoots {
		if !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
		if real, err := filepath.EvalSymlinks(root); err == nil {
			real = filepath.Clean(real)
			if !seen[real] {
				seen[real] = true
				out = append(out, real)
			}
		}
	}
	return out
}

func runtimePathAllowed(path string, trustedRoots []string) bool {
	roots := paths.CleanAbsolute(append(append([]string{}, standardLibraryDirs...), trustedRoots...))
	return paths.IsWithinAnyRoot(path, roots)
}
