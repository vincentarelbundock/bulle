package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/config"
)

const usageBeforeProfiles = `bulle runs local coding agents inside a controlled workspace.

It exposes one CLI and one policy model across Linux and macOS. Linux uses an
embedded Landlock backend; macOS uses a generated Seatbelt profile. The sandbox
restricts filesystem and environment access, and can optionally deny network access.

Usage:
  bulle [flags] [workspace] [-- command [args...]]
  bulle help | version

The workspace is the command's working directory and writable area. If omitted,
it defaults to the current directory. Use --no-workspace to run without the
automatic read-write workspace grant.
If no command is given after --, the configured default_app is used.

Filesystem flags (repeatable, comma-separated; executables must be allowed
explicitly with --rox or --rwx):
  --ro path        grant read-only access
  --rox path       grant read-only access plus execute
  --rw path        grant read-write access
  --rwx path       grant read-write access plus execute
  --no-workspace   do not automatically grant the workspace read-write access

Environment flags (no variables are passed unless requested):
  --env NAME        pass NAME from the current environment, if it is set
  --env NAME=VALUE  set NAME to VALUE inside the sandbox

Network flags:
  --no-network      deny network access for this invocation

Configuration:
  --config PATH     path to a configuration file

Profiles:
  -p, --profile NAME
                    named profile from the configuration file
  --list-profiles  list available profiles and exit
`

const usageAfterProfiles = `
Executable discovery:
  --add-exec        add the resolved command executable to the sandbox
  --add-libs        add runtime library access for executables
                    Linux discovers ELF shared-library dependencies precisely.
                    macOS uses configured runtime roots; Mach-O dylibs in
                    unusual locations may need explicit --ro/--rox grants.
                    These do not add app state files or environment variables.

Output and safety:
  --policy[=summary|json]
                    print the resolved policy and exit; summary by default
  -h, --help        show this help and exit
  -V, --version     show version information and exit

Examples:
  bulle --profile claude ~/repos/project   # Claude Code in a workspace
  bulle --add-exec -- /bin/ls
  bulle --profile codex --ro ~/.cache/uv
  bulle . --profile secrets --env OPENAI_API_KEY -- codex
  bulle . --rox /bin --policy=json -- /bin/bash
  bulle --profile codex --policy
  bulle --profile codex --no-network
`

var (
	profileDescriptions = map[string]string{
		"tool":     "general local command support (PATH, executables, libs)",
		"claude":   "Claude Code app state, config, and login support",
		"codex":    "Codex app state, config, network, MCP, and login support",
		"pi":       "Pi app state and config support",
		"opencode": "OpenCode app state and config support",
	}

	profileSortOrder = map[string]int{
		"tool":     0,
		"claude":   1,
		"codex":    2,
		"pi":       3,
		"opencode": 4,
	}
)

// Usage returns the full help text printed for --help and the help subcommand.
func Usage() string {
	return usageBeforeProfiles + profileUsage() + usageAfterProfiles
}

func profileUsage() string {
	cfg := config.DefaultConfig()
	names := ProfileNames(cfg)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "  %-17s %s\n", name, profileDescription(name, cfg.Profiles[name]))
	}
	return b.String()
}

// ProfileNames returns user-facing profile names in the same stable order used
// by help output: built-ins first, then custom names alphabetically.
func ProfileNames(cfg config.Config) []string {
	var names []string
	for name := range cfg.Profiles {
		if name != "default" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		leftOrder, leftPreferred := profileSortOrder[names[i]]
		rightOrder, rightPreferred := profileSortOrder[names[j]]
		if leftPreferred && rightPreferred {
			return leftOrder < rightOrder
		}
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		return names[i] < names[j]
	})
	return names
}

func profileDescription(name string, profile config.Profile) string {
	if description, ok := profileDescriptions[name]; ok {
		return description
	}
	if profile.DefaultApp != "" {
		return profile.DefaultApp + " app support"
	}
	return "built-in profile"
}
