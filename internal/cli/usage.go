package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/config"
)

const usageText = `bulle runs coding agents and other dangerous tools inside a sandbox.

Usage:
  bulle <profile>[,profile...] [dir] [-- command [args...]]

Everything before -- is policy; everything after -- is the command.
The profile's default app runs when no command is given. The optional dir is
the workspace: the command's working directory and writable area (default:
the current directory).

  bulle claude                  Claude Code, sandboxed, in this directory
  bulle claude ~/repos/x        the same, elsewhere
  bulle claude,offline          merge profiles left to right; offline denies network
  bulle -- pandoc doc.md        any command, minimal sandbox, binary auto-granted
  bulle git,network -- ./x.sh   profiles as grants for another command

Ad-hoc grants (repeatable; compose with the profile):
  --ro PATH    read           --rox PATH   read + execute
  --rw PATH    read + write   --rwx PATH   read + write + execute
  --env NAME[=VALUE]          pass or set an environment variable

When a run is denied something, bulle ends by showing the grants that would
have fixed it, and offers to save them to your profile.

Subcommands:
  bulle scratch <profile> [dir] [-- command]   run in a disposable clone
  bulle scratch list|diff|pull|wipe|shell      review kept scratches
  bulle show [policy|profiles|resolvers|config]
                                               inspect without running
  bulle profiles install SOURCE                install profiles (file, dir, git, github:)
  bulle completion bash|zsh|fish               shell completion
  bulle help [grants|env|limits|config]        the advanced material

Run "bulle help grants" for path syntax (?, +, which:, pkg:, resolvers,
variables), "bulle help env" for env files and globs, "bulle help limits" for
timeouts and resource caps, "bulle help config" for configuration, and
"bulle show profiles" for the profiles available on this machine.
`

// Usage returns the help text printed for --help and the help subcommand.
func Usage() string {
	return usageText
}

// ProfileListing renders the available profiles with their descriptions, for
// `bulle show profiles`.
func ProfileListing(cfg config.Config) string {
	var b strings.Builder
	for _, name := range ProfileNames(cfg) {
		fmt.Fprintf(&b, "  %-17s %s\n", name, profileDescription(name, cfg))
	}
	return b.String()
}

// ProfileNames returns user-facing profile names in the same stable order used
// by help output: alphabetically.
func ProfileNames(cfg config.Config) []string {
	var names []string
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func profileDescription(name string, cfg config.Config) string {
	if description := cfg.ProfileMetadata[name].Description; description != "" {
		return description
	}
	if profile := cfg.Profiles[name]; profile.DefaultApp != "" {
		return profile.DefaultApp + " app support"
	}
	return "custom profile"
}
