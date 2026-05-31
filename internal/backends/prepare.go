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
		for _, executable := range linuxELFDependencyRoots(prepared.Command[0]) {
			deps, err := elfdeps.GetLibraryDependencies(executable, elfdeps.DependencyOptions{TrustedRpathRoots: executableRoots(prepared)})
			if err != nil {
				return prepared, err
			}
			prepared.ReadOnlyExec = appendAbsolutePaths(prepared.ReadOnlyExec, deps...)
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
