//go:build linux || darwin

// Package trustedexec resolves helper executables that Bulle must run outside
// the target sandbox. Such helpers are authority-bearing code: neither the
// executable nor any directory used to reach its real path may be writable by
// the current identity.
package trustedexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Resolve returns path's canonical spelling when the executable and every
// ancestor of that canonical path are immutable to the current identity.
func Resolve(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	real = filepath.Clean(real)
	for current := real; ; current = filepath.Dir(current) {
		if err := unix.Access(current, unix.W_OK); err == nil {
			return "", fmt.Errorf("%s is writable by the current user", current)
		} else if !os.IsPermission(err) {
			return "", fmt.Errorf("check whether %s is writable: %w", current, err)
		}
		if current == string(filepath.Separator) {
			break
		}
	}
	return real, nil
}

// LookPath resolves the first executable named file on path. If that entry is
// mutable it fails closed instead of silently skipping to a later executable:
// a caller's PATH order must never change which privileged helper runs.
func LookPath(file string, path string) (string, error) {
	if strings.ContainsRune(file, filepath.Separator) {
		return Resolve(file)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, file)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return Resolve(candidate)
	}
	return "", fmt.Errorf("%q not found on PATH", file)
}

// First returns the first existing immutable executable from fixed candidates.
func First(candidates ...string) (string, error) {
	var last error
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		resolved, err := Resolve(candidate)
		if err == nil {
			return resolved, nil
		}
		last = err
	}
	if last != nil {
		return "", last
	}
	return "", fmt.Errorf("no trusted executable found in %v", candidates)
}
