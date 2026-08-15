package config

import (
	"os"
	"path/filepath"
)

// DefaultRoot is the directory bulle reads configuration from when no --config
// was given: the platform user configuration directory plus "bulle". It
// returns "" when the platform cannot name one, which callers report as an
// error rather than falling back to a guess.
func DefaultRoot() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "bulle")
}
