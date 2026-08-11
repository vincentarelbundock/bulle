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
  bulle policy [run flags] [--json]
  bulle rerun [extra flags]
  bulle scratch list|diff|pull|wipe|shell [id]
  bulle profiles list|install SOURCE
  bulle resolvers
  bulle help | version

The workspace is the command's working directory and writable area. If omitted,
it defaults to the current directory. Use --no-workspace to run without the
automatic read-write workspace grant.
If no command is given after --, the configured default_app is used.
The -- separator may be omitted when unambiguous: the first positional that is
an existing directory reads as the workspace, and the first positional that is
not starts the command (announced on stderr).

Filesystem flags (repeatable, comma-separated; executables must be allowed
explicitly with --rox or --rwx):
  --ro path        grant read-only access
  --rox path       grant read-only access plus execute
  --rw path        grant read-write access
  --rwx path       grant read-write access plus execute
  --no-workspace   do not automatically grant the workspace read-write access

Path entries (flags and profiles):
  ?path            optional: skip silently when the path does not exist
  +path            create an empty file when missing (rw/rwx entries)
  +path/           create a directory when missing (rw/rwx entries)
  which:NAME       resolve NAME on the parent PATH and grant that binary only,
                   findable by name inside the sandbox (rox/rwx entries)
  pkg:NAME         like which:, plus the tool's package tree (rox/rwx entries)
  TOOL:ASPECT      ask a tool where its own directories are, such as r:libs or
                   uv:cache; valid in every list (see bulle resolvers)
  dir/*/bin        single-star glob; no matches means the entry is skipped
  Variables: $WORKSPACE, $HOME, $TMP, $TMPDIR, and the per-platform
  $CONFIG, $DATA, $CACHE, $STATE (XDG dirs on Linux, ~/Library on macOS).
  ${NAME:-fallback} falls back when NAME is unset.

Environment flags (no variables are passed unless requested):
  --env NAME        pass NAME from the current environment, if it is set
  --env NAME=VALUE  set NAME to VALUE inside the sandbox
  --env 'GLOB'      pass every parent variable matching a name glob (GIT_*)
  --env-file PATH   load NAME=VALUE entries from a dotenv-style file
  --env-all-except NAME,...
                    pass the whole parent environment except the named variables

Subcommands:
  policy            resolve and print the policy without running anything;
                    accepts every run flag, plus --json (summary otherwise)
  rerun             repeat the previous invocation, with extra flags inserted
                    before the command (e.g. bulle rerun --ro ~/.gitconfig)
  scratch           manage kept scratch workspaces (see below)
  profiles list     list available profiles
  profiles install SOURCE
                    install profile TOML files from a file, directory, local
                    git repository, or GitHub source
  resolvers         list path resolvers and what they resolve to here

Configuration:
  --config PATH     path to a configuration directory
  --var NAME=VALUE  define a custom path variable usable in grants as $NAME
  --no-defaults     ignore the [defaults] block of the user configuration

Profiles:
  -p, --profile NAME
                    named profile, or comma-separated profiles merged left to right
`

const usageAfterProfiles = `
Executable discovery:
  --add-exec        add the resolved command executable to the sandbox
  --add-libs        add runtime library access for executables
                    Linux discovers ELF shared-library dependencies precisely.
                    macOS uses configured runtime roots; Mach-O dylibs in
                    unusual locations may need explicit --ro/--rox grants.
                    These do not add app state files or environment variables.

Disposable workspaces:
  --scratch         run against a disposable local clone of the workspace
                    (git repositories only; uncommitted work is carried over).
                    After the run, review the changes: diff, pull into the
                    origin, keep, open a shell in the scratch, or wipe it.
  --scratch-keep    skip the review prompt, keep the scratch, print its path
  bulle scratch list|diff|pull|wipe|shell [id]
                    resume the review of a kept scratch later; id may be a
                    unique prefix, and is optional when unambiguous

Output and safety:
  --timeout DURATION
                    kill the sandboxed command if it runs longer than DURATION.
                    Uses Go duration syntax such as 30s, 2m, or 1h30m.
                    Use 0 to disable. Timed-out commands exit 124.
  -h, --help        show this help and exit
  -V, --version     show version information and exit

Examples:
  bulle --profile claude ~/repos/project   # Claude Code in a workspace
  bulle --add-exec -- /bin/ls
  bulle --profile codex --ro ~/.cache/uv
  bulle . --profile secrets --env OPENAI_API_KEY -- codex
  bulle policy --json . --rox /bin -- /bin/bash
  bulle policy --profile codex
  bulle --profile codex,offline
  bulle profiles install agent.toml
  bulle profiles install github:vincentarelbundock/bulle/custom_profiles
`

// Usage returns the full help text printed for --help and the help subcommand.
func Usage() string {
	return usageBeforeProfiles + profileUsage() + usageAfterProfiles
}

func profileUsage() string {
	cfg := config.DefaultConfig()
	names := ProfileNames(cfg)
	var b strings.Builder
	for _, name := range names {
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
