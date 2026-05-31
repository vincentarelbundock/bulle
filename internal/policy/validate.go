package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

var ErrCommandNotFound = errors.New("command not found")

func PrepareCommandExecutable(p Policy) (Policy, error) {
	out := p
	if len(p.Command) == 0 {
		return out, nil
	}
	out.Command = append([]string{}, p.Command...)
	command := p.Command[0]
	binary, err := lookPath(command, p.Env["PATH"], p.ProjectPath)
	if err != nil {
		return out, fmt.Errorf("%w before sandbox setup: %q is not on policy PATH. Add --env PATH with matching --rox/--rwx roots, choose a profile, or pass an explicit executable path after --", ErrCommandNotFound, command)
	}
	binary = filepath.Clean(binary)
	out.Command[0] = binary

	if p.AddExec {
		out.ReadOnlyExec = appendExecutablePath(out.ReadOnlyExec, binary)
		if real, err := filepath.EvalSymlinks(binary); err == nil {
			out.ReadOnlyExec = appendExecutablePath(out.ReadOnlyExec, real)
		}
		sanitizePolicyPATH(out.Env, append(append([]string{}, out.ReadOnlyExec...), out.ReadWriteExec...))
		return out, nil
	}

	if bpaths.IsWithinAnyRootResolvingSymlinks(binary, append(append([]string{}, p.ReadOnlyExec...), p.ReadWriteExec...)) {
		return out, nil
	}
	return out, fmt.Errorf(
		"command is not executable under current filesystem policy: %q resolves to %q, but no --rox or --rwx path allows it. Add --rox %s or select --profile tool",
		command,
		binary,
		filepath.Dir(binary),
	)
}

func appendExecutablePath(paths []string, path string) []string {
	if path == "" || !filepath.IsAbs(path) {
		return paths
	}
	clean := filepath.Clean(path)
	for _, existing := range paths {
		if existing == clean {
			return paths
		}
	}
	return append(paths, clean)
}

func lookPath(command string, policyPATH string, projectPath string) (string, error) {
	if strings.Contains(command, string(filepath.Separator)) {
		path := command
		if !filepath.IsAbs(path) {
			if projectPath != "" {
				path = filepath.Join(projectPath, path)
			} else {
				abs, err := filepath.Abs(path)
				if err != nil {
					return "", err
				}
				path = abs
			}
		}
		if isExecutableFile(path) {
			return filepath.Clean(path), nil
		}
		return "", ErrCommandNotFound
	}
	for _, dir := range filepath.SplitList(policyPATH) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, command)
		if isExecutableFile(candidate) {
			return filepath.Clean(candidate), nil
		}
	}
	return "", ErrCommandNotFound
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
