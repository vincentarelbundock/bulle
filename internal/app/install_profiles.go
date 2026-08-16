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

// installProfiles copies profile files from a local directory or a GitHub
// repository into the user's profile directory.
//
// An installed profile is code in every sense that matters here: it decides
// what a sandbox grants, and a file whose name matches a built-in profile
// merges into it — so a plausible-looking node.toml can widen every profile
// that inherits from node, on runs that never mention node. So the install is
// neither silent nor destructive: what each file grants is printed, an existing
// file is never replaced without --force, and shadowing a built-in name is
// called out.
func installProfiles(source string, configRoot string, force bool, stdout io.Writer) error {
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
		name    string
		base    string
		data    []byte
		profile config.Profile
	}
	installFiles := make([]installFile, 0, len(files))
	for _, sourceFile := range files {
		name, profile, err := validateInstallProfileFile(sourceFile)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(sourceFile)
		if err != nil {
			return err
		}
		installFiles = append(installFiles, installFile{name: name, base: filepath.Base(sourceFile), data: data, profile: profile})
	}

	profileDir := filepath.Join(configRoot, "profiles")
	builtIn := config.DefaultConfig().Profiles
	var existing []string
	for _, file := range installFiles {
		if _, err := os.Stat(filepath.Join(profileDir, file.base)); err == nil {
			existing = append(existing, file.base)
		}
	}
	if len(existing) > 0 && !force {
		return fmt.Errorf("refusing to replace profiles already installed here: %s\n"+
			"review %s first, then re-run with --force to overwrite",
			strings.Join(existing, ", "), profileDir)
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}
	for _, file := range installFiles {
		dest := filepath.Join(profileDir, file.base)
		if err := os.WriteFile(dest, file.data, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "installed %s\n", file.name)
		if _, shadows := builtIn[file.name]; shadows {
			fmt.Fprintf(stdout, "  warning: %q is a built-in profile; this file merges into it, and into every profile that inherits from it\n", file.name)
		}
		for _, line := range describeInstalledGrants(file.profile) {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
	}
	return nil
}

// describeInstalledGrants renders the filesystem entries a profile file
// declares, so installing is not a decision made sight unseen. Entries are
// printed as written; resolving them would need the machine state a run has.
func describeInstalledGrants(profile config.Profile) []string {
	var out []string
	for _, block := range []struct {
		label    string
		settings config.Settings
	}{{"", profile.Settings}, {"linux ", profile.Linux}, {"macos ", profile.MacOS}} {
		for _, list := range []struct {
			name    string
			entries []string
		}{
			{"ro", block.settings.ReadOnly}, {"rox", block.settings.ReadOnlyExec},
			{"rw", block.settings.ReadWrite}, {"rwx", block.settings.ReadWriteExec},
		} {
			if len(list.entries) > 0 {
				out = append(out, fmt.Sprintf("%s%-4s %s", block.label, list.name, strings.Join(list.entries, ", ")))
			}
		}
		if len(block.settings.Deny) > 0 {
			out = append(out, block.label+"deny "+strings.Join(block.settings.Deny, ", "))
		}
		if len(block.settings.Allow) > 0 {
			out = append(out, block.label+"allow "+strings.Join(block.settings.Allow, ", "))
		}
	}
	if len(profile.Inherits.Names) > 0 {
		out = append(out, "inherits "+strings.Join(profile.Inherits.Names, ", "))
	}
	return out
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

func validateInstallProfileFile(path string) (string, config.Profile, error) {
	name, profile, _, err := config.LoadProfileFile(path)
	if err != nil {
		return "", config.Profile{}, err
	}
	return name, profile, nil
}
