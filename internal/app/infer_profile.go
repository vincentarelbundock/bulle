package app

import (
	"path/filepath"
	"sort"

	"github.com/google/shlex"
	"github.com/vincentarelbundock/bulle/internal/config"
)

// profilesMatchingCommand returns the names of installed profiles whose
// default_app runs the given command (compared by executable basename, so
// "codex" matches default_app = "/usr/bin/codex --yolo"). The default profile
// is excluded: selecting it would be a no-op.
func profilesMatchingCommand(global config.Config, command string) []string {
	base := filepath.Base(command)
	var names []string
	for name, profile := range global.Profiles {
		if name == "default" || profile.DefaultApp == "" {
			continue
		}
		words, err := shlex.Split(profile.DefaultApp)
		if err != nil || len(words) == 0 {
			continue
		}
		if filepath.Base(words[0]) == base {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
