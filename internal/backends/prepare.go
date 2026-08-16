package backends

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/elfdeps"
	"github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func PreparePolicy(p policy.Policy) (policy.Policy, error) {
	prepared, err := policy.PrepareCommandExecutable(p)
	if err != nil {
		return prepared, err
	}
	if len(prepared.Command) == 0 {
		return prepared, nil
	}
	if supportsShebangPreparation(prepared.Backend) {
		if err := prepareShebangInterpreter(&prepared); err != nil {
			return prepared, err
		}
	}
	if prepared.Backend == policy.BackendLinuxLandlock && prepared.AddLibs {
		// Seeds are the command, its shebang interpreter, and every ELF object
		// found inside the granted trees. The tree scan is what handles
		// interpreters reached through wrapper scripts: the wrapper has no ELF
		// dependencies, but the real binary lives in a granted tree (an R
		// prefix, a Nix store path) and its runtime libraries are discovered
		// from there. Package store roots are trusted for RPATH/RUNPATH
		// resolution so those libraries resolve even before they are granted.
		scan := scanTreesForELFSeeds(scannableExecutableRoots(prepared), prepared.ReadOnly)
		seeds := append(linuxELFDependencyRoots(prepared.Command[0]), scan.seeds...)
		// Store references chain: a wrapper script references a store item
		// whose nix-support files reference further items (a cc wrapper
		// naming libgcc). Follow a few hops, never revisiting an item.
		granted := map[string]bool{}
		refs := scan.storeRefs
		for hop := 0; hop < 3 && len(refs) > 0; hop++ {
			fresh := []string{}
			for _, ref := range refs {
				if !granted[ref] {
					granted[ref] = true
					fresh = append(fresh, ref)
				}
			}
			if len(fresh) == 0 {
				break
			}
			prepared.ReadOnlyExec = appendAbsolutePaths(prepared.ReadOnlyExec, fresh...)
			refScan := scanTreesForELFSeeds(fresh, nil)
			seeds = append(seeds, refScan.seeds...)
			refs = refScan.storeRefs
		}
		for _, interpreter := range scan.interpreters {
			// A scanned script's shebang can name a path that does not exist
			// on this machine (a portable script's #!/bin/bash on NixOS);
			// granting it would fail ruleset population.
			real, err := filepath.EvalSymlinks(interpreter)
			if err != nil {
				continue
			}
			prepared.ReadOnlyExec = appendAbsolutePaths(prepared.ReadOnlyExec, interpreter, real)
			seeds = append(seeds, real)
		}
		trusted := append(scannableExecutableRoots(prepared), packageStoreRoots...)
		deps, err := elfdeps.GetLibraryDependenciesForAll(seeds, elfdeps.DependencyOptions{TrustedRpathRoots: trusted})
		if err != nil {
			return prepared, err
		}
		prepared.ReadOnlyExec = appendAbsolutePaths(prepared.ReadOnlyExec, deps...)
		// The dynamic loader can carry baked-in default search directories
		// (Nix patches ld.so with a libgcc path for lazy unwinder loads that
		// appear in no ELF header). Treat store paths embedded in the loader
		// like a wrapper script's references.
		for _, dep := range deps {
			if strings.HasPrefix(filepath.Base(dep), "ld-") {
				for _, ref := range storeRefsFromFile(dep) {
					prepared.ReadOnlyExec = appendAbsolutePaths(prepared.ReadOnlyExec, ref)
				}
			}
		}
		// A library's package often carries data its code reads at runtime
		// (glibc's gconv tables and locale archive, ICU data). Inside a
		// package store, grant the whole store item read-only alongside the
		// library file itself.
		for _, dep := range deps {
			if root, ok := storeItemRootFor(dep); ok {
				prepared.ReadOnly = appendAbsolutePaths(prepared.ReadOnly, root)
			}
		}
		if scan.truncated {
			prepared.Notes = append(prepared.Notes,
				"add_libs: the library scan hit its size budget, so some runtime libraries may be missing; a failed run will hint the rest")
		}
	}
	return prepared, nil
}

func supportsShebangPreparation(backend policy.BackendName) bool {
	return backend == policy.BackendLinuxLandlock || backend == policy.BackendMacOSSeatbelt
}

func prepareShebangInterpreter(p *policy.Policy) error {
	interpreter, ok := shebangInterpreter(p.Command[0])
	if !ok {
		return nil
	}
	if p.AddExec {
		p.ReadOnlyExec = appendAbsolutePaths(p.ReadOnlyExec, interpreter)
		if real, err := filepath.EvalSymlinks(interpreter); err == nil {
			p.ReadOnlyExec = appendAbsolutePaths(p.ReadOnlyExec, real)
		}
		for _, script := range shebangScriptPaths(p.Command[0]) {
			dir := filepath.Dir(script)
			if packageRoot, ok := nearestPackageRoot(dir); ok {
				p.ReadOnlyExec = appendAbsolutePaths(p.ReadOnlyExec, packageRoot)
			} else {
				p.ReadOnly = appendAbsolutePaths(p.ReadOnly, dir)
			}
		}
		return nil
	}
	if executablePathAllowed(interpreter, *p) {
		return nil
	}
	return fmt.Errorf("%w before sandbox setup: script interpreter %q is not executable under current filesystem policy. Add --rox %s or enable --add-exec", policy.ErrCommandNotFound, interpreter, filepath.Dir(interpreter))
}

func nearestPackageRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func shebangScriptPaths(path string) []string {
	out := []string{}
	if path == "" || !filepath.IsAbs(path) {
		return out
	}
	out = append(out, filepath.Clean(path))
	if real, err := filepath.EvalSymlinks(path); err == nil {
		out = appendAbsolutePaths(out, real)
	}
	return out
}

func linuxELFDependencyRoots(command string) []string {
	roots := []string{command}
	if interpreter, ok := shebangInterpreter(command); ok {
		roots = append(roots, interpreter)
	}
	return roots
}

func shebangInterpreter(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", false
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "#!") {
		return "", false
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
	if len(fields) == 0 || !filepath.IsAbs(fields[0]) {
		return "", false
	}
	return filepath.Clean(fields[0]), true
}

func executablePathAllowed(path string, p policy.Policy) bool {
	return paths.IsWithinAnyRootResolvingSymlinks(path, paths.CleanAbsolute(executableRoots(p)))
}

func executableRoots(p policy.Policy) []string {
	return append(append([]string{}, p.ReadOnlyExec...), p.ReadWriteExec...)
}

// scannableExecutableRoots are the executable trees library discovery may read
// as evidence. Read-write-exec trees are excluded on purpose: their contents
// are chosen by the confined process, so a shebang or a store reference found
// in one is not a fact about the machine but an instruction from the sandbox —
// and following it would let a run pick what the next run grants FS_EXECUTE on.
// Deciding whether the command itself is allowed to run still consults every
// executable root; that is a question about what was granted, not about what
// the grant's contents ask for.
func scannableExecutableRoots(p policy.Policy) []string {
	return append([]string{}, p.ReadOnlyExec...)
}

func appendAbsolutePaths(paths []string, extra ...string) []string {
	for _, path := range extra {
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		clean := filepath.Clean(path)
		if containsPath(paths, clean) {
			continue
		}
		paths = append(paths, clean)
	}
	return paths
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
