package backends

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Library discovery budget. The scan is proportional to the size of the
// granted trees, so it is bounded three ways: total directory entries visited,
// total ELF seeds collected, and wall-clock time. Hitting any bound stops the
// scan and is surfaced as a policy note rather than silently dropped.
const (
	libScanMaxEntries   = 300000
	libScanMaxSeeds     = 6000
	libScanTimeBudget   = 15 * time.Second
	libScanLibsMaxDepth = 6
)

// packageStoreRoots are directory trees owned by content-addressed or
// per-package installers. A binary's RPATH/RUNPATH pointing inside one of
// these names a library curated by the same build that produced the binary,
// so following it during add_libs discovery does not widen trust: the binary
// itself is already granted and would load exactly that library.
var packageStoreRoots = []string{
	"/nix/store",
	"/gnu/store",
	"/opt/homebrew/Cellar",
	"/usr/local/Cellar",
}

// libScanSkipRoots are trees never worth walking for ELF seeds: system
// directories whose libraries are already covered by the standard library
// grants, and pseudo-filesystems.
var libScanSkipRoots = []string{
	"/", "/bin", "/sbin", "/etc", "/var", "/tmp", "/dev", "/proc", "/sys",
	"/run", "/usr", "/usr/bin", "/usr/sbin", "/usr/lib", "/usr/lib64",
	"/usr/libexec", "/usr/share", "/usr/include", "/lib", "/lib64",
	"/System", "/Applications", "/Library",
}

// storeItemComponents is the path depth of a package's own root below each
// package store: /nix/store/<item> is 3 components, Cellar/<name>/<version>
// is 5.
var storeItemComponents = map[string]int{
	"/nix/store":           3,
	"/gnu/store":           3,
	"/opt/homebrew/Cellar": 5,
	"/usr/local/Cellar":    5,
}

// storeItemRootFor returns the enclosing package-store item root for a path
// inside a package store, or false for paths anywhere else.
func storeItemRootFor(path string) (string, bool) {
	for _, storeRoot := range packageStoreRoots {
		if !strings.HasPrefix(path, storeRoot+"/") {
			continue
		}
		components := storeItemComponents[storeRoot]
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) <= components {
			return "", false
		}
		return "/" + strings.Join(parts[:components], "/"), true
	}
	return "", false
}

type libScanResult struct {
	seeds []string
	// interpreters are shebang interpreters of executable scripts found in the
	// exec trees. A wrapper script inside a granted tree (Nix R, version
	// managers) execs an interpreter that may live outside every grant; it
	// must be granted and its own libraries discovered.
	interpreters []string
	// storeRefs are package-store roots that executable scripts in the exec
	// trees reference by absolute path (a Nix R wrapper calling
	// /nix/store/…-gnused/bin/sed). The script will exec them, so they need
	// the same treatment as the tree the script came from.
	storeRefs []string
	truncated bool
}

// scanTreesForELFSeeds finds the ELF objects a policy's granted trees contain,
// so their runtime dependencies can be granted too. This is what makes
// add_libs work for interpreters reached through wrapper scripts: ELF
// discovery on the wrapper finds nothing, but the real binary several exec
// hops down lives inside a granted tree (an R prefix, a Nix store path) and
// is found here.
//
// Exec trees are walked fully. Read-only trees are walked only for files
// under a directory named "libs" — the R package convention for compiled
// code — so a large data or cache grant does not cost a full traversal of
// its ELF-free contents.
func scanTreesForELFSeeds(execRoots []string, readRoots []string) libScanResult {
	deadline := time.Now().Add(libScanTimeBudget)
	seen := map[string]bool{}
	visited := 0
	result := libScanResult{}

	walk := func(root string, libsOnly bool) {
		clean, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			return
		}
		if seen["root:"+clean] || libScanSkipRoot(clean) {
			return
		}
		seen["root:"+clean] = true
		info, err := os.Stat(clean)
		if err != nil {
			return
		}
		if !info.IsDir() {
			if seen[clean] {
				return
			}
			seen[clean] = true
			if isELFFile(clean) {
				result.seeds = append(result.seeds, clean)
			} else if interp, ok := shebangInterpreter(clean); ok {
				if !seen["interp:"+interp] {
					seen["interp:"+interp] = true
					result.interpreters = append(result.interpreters, interp)
				}
				result.addStoreRefs(scriptStoreRefs(clean), seen)
			}
			return
		}
		filepath.WalkDir(clean, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			visited++
			if visited > libScanMaxEntries || len(result.seeds) >= libScanMaxSeeds || time.Now().After(deadline) {
				result.truncated = true
				return filepath.SkipAll
			}
			if entry.IsDir() {
				if libsOnly && pathDepthBelow(clean, path) > libScanLibsMaxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				// A store item can point into another item by symlink (gcc's
				// lib/libgcc_s.so.1 links to a separate libgcc output). The
				// target item needs granting like a textual reference.
				if target, err := filepath.EvalSymlinks(path); err == nil {
					if root, ok := storeItemRootFor(target); ok && root != clean {
						result.addStoreRefs([]string{root}, seen)
					}
				}
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			if libsOnly && !underLibsDir(clean, path) {
				return nil
			}
			// nix-support files are the Nix convention for propagated build
			// inputs: plain-text flag files naming the store paths a wrapper
			// injects at link time (libgcc, crt objects). They are neither
			// executable nor ELF, so they need their own carve-out.
			if !libsOnly && filepath.Base(filepath.Dir(path)) == "nix-support" {
				if !seen[path] {
					seen[path] = true
					result.addStoreRefs(scriptStoreRefs(path), seen)
				}
				return nil
			}
			if !libScanCandidateName(entry) {
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			if isELFFile(path) {
				result.seeds = append(result.seeds, path)
			} else if !libsOnly {
				if interp, ok := shebangInterpreter(path); ok {
					if !seen["interp:"+interp] {
						seen["interp:"+interp] = true
						result.interpreters = append(result.interpreters, interp)
					}
					result.addStoreRefs(scriptStoreRefs(path), seen)
				}
			}
			return nil
		})
	}

	for _, root := range sortedUnique(execRoots) {
		walk(root, false)
	}
	for _, root := range sortedUnique(readRoots) {
		walk(root, true)
	}
	sort.Strings(result.seeds)
	sort.Strings(result.interpreters)
	sort.Strings(result.storeRefs)
	return result
}

func (r *libScanResult) addStoreRefs(refs []string, seen map[string]bool) {
	for _, ref := range refs {
		if seen["ref:"+ref] {
			continue
		}
		seen["ref:"+ref] = true
		// Prose can contain things that look like store paths ("/nix/store/..."
		// in a comment); only real items become grants.
		if info, err := os.Stat(ref); err == nil && info.IsDir() {
			r.storeRefs = append(r.storeRefs, ref)
		}
	}
}

// scriptStoreRefsMaxSize bounds how much of a file is read when looking for
// package-store references. Wrapper scripts are a few KB; the dynamic loader,
// also scanned for its baked-in default directories, is a few hundred KB.
const scriptStoreRefsMaxSize = 4 * 1024 * 1024

// storeRefsFromFile returns the existing package-store items a file references,
// for callers outside the tree walk (which filters through addStoreRefs).
func storeRefsFromFile(path string) []string {
	out := []string{}
	for _, ref := range scriptStoreRefs(path) {
		if info, err := os.Stat(ref); err == nil && info.IsDir() {
			out = append(out, ref)
		}
	}
	return out
}

// scriptStoreRefs returns the package-store item roots a script references by
// absolute path. Only roots under packageStoreRoots are reported: those trees
// are content-addressed and curated by the same build that produced the
// script, so granting one follows trust the script's own grant already
// established.
func scriptStoreRefs(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > scriptStoreRefsMaxSize {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	refs := []string{}
	seen := map[string]bool{}
	text := string(data)
	for _, storeRoot := range packageStoreRoots {
		components := storeItemComponents[storeRoot]
		for start := 0; ; {
			i := strings.Index(text[start:], storeRoot+"/")
			if i < 0 {
				break
			}
			i += start
			end := i
			for end < len(text) && isStorePathChar(text[end]) {
				end++
			}
			start = i + len(storeRoot) + 1
			parts := strings.Split(strings.TrimPrefix(text[i:end], "/"), "/")
			if len(parts) <= components {
				continue
			}
			root := "/" + strings.Join(parts[:components], "/")
			if !seen[root] {
				seen[root] = true
				refs = append(refs, root)
			}
		}
	}
	return refs
}

// isStorePathChar reports bytes that can appear in a store path. Anything
// else ends the match, which keeps the scan correct inside binaries where
// paths are separated by arbitrary non-path bytes rather than whitespace.
func isStorePathChar(c byte) bool {
	return c == '/' || c == '.' || c == '-' || c == '_' || c == '+' ||
		'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}

// libScanCandidateName filters walk entries to files that can plausibly be
// ELF objects — executables and shared libraries — before paying for an open.
func libScanCandidateName(entry fs.DirEntry) bool {
	if strings.Contains(entry.Name(), ".so") {
		return true
	}
	info, err := entry.Info()
	return err == nil && info.Mode().Perm()&0o111 != 0
}

func libScanSkipRoot(path string) bool {
	for _, skip := range libScanSkipRoots {
		if path == skip {
			return true
		}
	}
	return false
}

func underLibsDir(root string, path string) bool {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "libs" {
			return true
		}
	}
	return false
}

func pathDepthBelow(root string, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

func isELFFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := f.Read(magic); err != nil {
		return false
	}
	return magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F'
}

func sortedUnique(paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
