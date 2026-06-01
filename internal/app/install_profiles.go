package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/config"
)

func installProfiles(source string, configRoot string, stdout io.Writer) error {
	resolved, cleanup, err := resolveInstallProfileSource(source)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	files, err := profileFilesForInstall(resolved)
	if err != nil {
		return err
	}
	type installFile struct {
		name string
		base string
		data []byte
	}
	installFiles := make([]installFile, 0, len(files))
	for _, sourceFile := range files {
		name, err := validateInstallProfileFile(sourceFile)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(sourceFile)
		if err != nil {
			return err
		}
		installFiles = append(installFiles, installFile{name: name, base: filepath.Base(sourceFile), data: data})
	}

	profileDir := filepath.Join(configRoot, "profiles")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}
	for _, file := range installFiles {
		dest := filepath.Join(profileDir, file.base)
		if err := os.WriteFile(dest, file.data, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "installed %s\n", file.name)
	}
	return nil
}

func resolveInstallProfileSource(source string) (string, func(), error) {
	if source == "" {
		return "", nil, fmt.Errorf("--install-profiles requires a source")
	}
	if _, err := os.Stat(source); err == nil {
		return source, nil, nil
	} else if !os.IsNotExist(err) {
		return "", nil, err
	}

	if repo, subdir, ok := parseGitHubProfileInstallSource(source); ok {
		return cloneProfileRepository(repo, subdir)
	}
	return "", nil, fmt.Errorf("profile source %q does not exist", source)
}

func cloneProfileRepository(repo string, subdir string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "bulle-profiles-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	checkout := filepath.Join(tmp, "repo")
	cmd := exec.Command("git", "clone", "--depth", "1", repo, checkout)
	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("clone profile repository %q: %w\n%s", repo, err, strings.TrimSpace(string(output)))
	}
	if subdir != "" {
		checkout = filepath.Join(checkout, filepath.FromSlash(subdir))
	}
	return checkout, cleanup, nil
}

func parseGitHubProfileInstallSource(source string) (string, string, bool) {
	path, ok := strings.CutPrefix(source, "github:")
	if !ok || path == "" || strings.Contains(path, "://") || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || strings.HasPrefix(path, "~") {
		return "", "", false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 2 || !validGitHubPathPart(parts[0]) || !validGitHubPathPart(parts[1]) {
		return "", "", false
	}
	for _, part := range parts[2:] {
		if part == "" || part == "." || part == ".." {
			return "", "", false
		}
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if repo == "" {
		return "", "", false
	}
	subdir := strings.Join(parts[2:], "/")
	return "https://github.com/" + parts[0] + "/" + repo + ".git", subdir, true
}

func validGitHubPathPart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, r := range part {
		if r == '-' || r == '_' || r == '.' || '0' <= r && r <= '9' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' {
			continue
		}
		return false
	}
	return true
}

func profileFilesForInstall(source string) ([]string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if filepath.Ext(source) != ".toml" {
			return nil, fmt.Errorf("profile source file %s must be a .toml file", source)
		}
		return []string{source}, nil
	}

	dir := source
	profileSubdir := filepath.Join(source, "profiles")
	if isGitCheckout(source) && isDir(profileSubdir) {
		dir = profileSubdir
	} else if !hasDirectTOMLFiles(source) && isDir(profileSubdir) {
		dir = profileSubdir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("profile source %s contains no .toml files", source)
	}
	return files, nil
}

func isGitCheckout(path string) bool {
	return isDir(filepath.Join(path, ".git"))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func hasDirectTOMLFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".toml" {
			return true
		}
	}
	return false
}

func validateInstallProfileFile(path string) (string, error) {
	name, _, _, err := config.LoadProfileFile(path)
	if err != nil {
		return "", err
	}
	return name, nil
}
