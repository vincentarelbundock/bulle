package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/vincentarelbundock/bulle/internal/config"
	"github.com/vincentarelbundock/bulle/internal/exitcode"
)

// runConfigCommand reports which configuration files are in effect and where
// one would be created. loadConfig deliberately ignores a missing
// config.toml, so this is the one place a typo'd directory or an unread file
// becomes visible.
func runConfigCommand(rest []string, stdout, stderr io.Writer) int {
	root := ""
	switch {
	case len(rest) == 0:
		root = config.DefaultRoot()
	case len(rest) == 2 && rest[0] == "--config":
		root = rest[1]
	default:
		fmt.Fprintln(stderr, "usage: bulle config [--config PATH]")
		return exitcode.ConfigError
	}
	if root == "" {
		fmt.Fprintln(stderr, "bulle: cannot determine the user configuration directory")
		return exitcode.ConfigError
	}

	exit := exitcode.OK
	fmt.Fprintf(stdout, "configuration root: %s\n", root)

	configPath := filepath.Join(root, "config.toml")
	if _, err := config.LoadFile(configPath); err == nil {
		fmt.Fprintf(stdout, "  config.toml: loaded\n")
	} else if os.IsNotExist(err) {
		fmt.Fprintf(stdout, "  config.toml: not found; create it for machine-local settings ([defaults], [vars], [scratch])\n")
	} else {
		fmt.Fprintf(stdout, "  config.toml: ERROR: %v\n", err)
		exit = exitcode.ConfigError
	}

	profilesDir := filepath.Join(root, "profiles")
	if loaded, err := config.LoadProfileDirectory(profilesDir); err == nil {
		fmt.Fprintf(stdout, "  profiles/:   %d profile file(s) loaded\n", len(loaded.Profiles))
	} else if os.IsNotExist(err) {
		fmt.Fprintf(stdout, "  profiles/:   not found; put profile TOML files there, or use 'bulle profiles install'\n")
	} else {
		fmt.Fprintf(stdout, "  profiles/:   ERROR: %v\n", err)
		exit = exitcode.ConfigError
	}

	builtin, err := config.LoadDefaultConfig()
	if err == nil {
		fmt.Fprintf(stdout, "built-in profiles: %d (user profiles with the same name override them)\n", len(builtin.Profiles))
	}
	return exit
}
