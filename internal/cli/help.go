package cli

import "sort"

// CommandHelp returns focused help for one subcommand or help topic, printed
// for `bulle help <name>` and `bulle <name> --help`. The bool reports whether
// the topic exists; app tests assert every dispatched subcommand has an entry,
// so the topics cannot drift from the dispatch table.
func CommandHelp(name string) (string, bool) {
	text, ok := commandHelp[name]
	return text, ok
}

// HelpTopics lists the help-only topics — advanced material that is not a
// subcommand — for completion of `bulle help <topic>`.
func HelpTopics() []string {
	var out []string
	for name := range helpTopics {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var commandHelp = func() map[string]string {
	all := map[string]string{}
	for name, text := range subcommandHelp {
		all[name] = text
	}
	for name, text := range helpTopics {
		all[name] = text
	}
	return all
}()

var subcommandHelp = map[string]string{
	"show": `Inspect without running anything.

Usage:
  bulle show [policy] [run args...] [--json]
  bulle show profiles
  bulle show resolvers
  bulle show config [--config PATH]

show policy (the default) accepts everything a run does, so it prints exactly
what the same arguments would grant. Without a command, command-dependent
grants (executable discovery, shebang discovery) are simply absent.

show profiles lists the profiles available on this machine, built-in and
installed alike. show resolvers lists the TOOL:ASPECT path resolvers and what
they resolve to here. show config reports which configuration files are in
effect, and whether they load cleanly.

Examples:
  bulle show claude
  bulle show --json codex . -- codex
`,

	"scratch": `Run in a disposable clone of the workspace, or manage kept scratches.

Usage:
  bulle scratch <profile> [dir] [-- command [args...]]
  bulle scratch list|diff|pull|wipe|shell [id]

A scratch is a disposable local clone of a git workspace (uncommitted work is
carried over). After the run, review the changes: diff, pull into the origin,
keep, open a shell in the scratch, or wipe it. Everything after "scratch"
that is not a review verb is an ordinary run; use -- to run a command named
like a verb.

The review verbs resume a kept scratch later; id may be a unique prefix, and
is optional when unambiguous.

Related run flags:
  --scratch         run in a scratch, as a flag on an ordinary run
  --scratch-keep    skip the review prompt, keep the scratch, print its path
`,

	"profiles": `List available profiles or install new ones.

Usage:
  bulle profiles list
  bulle profiles install [--force] SOURCE

install accepts profile TOML files from a file, a directory, a local git
repository, or a GitHub source (github:owner/repo[/subdir]), and copies them
into your configuration directory's profiles/ folder. What each file grants
is printed as it is installed.

A profile file named after an existing profile merges into it: that is how
a file extends a built-in profile, and where the entries printed after a
denied run belong. Because that merge also reaches every profile inheriting
from the name, installing over a profile you already have is refused unless
you pass --force.

Examples:
  bulle profiles install agent.toml
  bulle profiles install github:vincentarelbundock/bulle/custom_profiles
`,

	"completion": `Print a shell completion script.

Usage:
  bulle completion bash|zsh|fish

The scripts are thin shims that ask the installed binary at completion time,
so subcommands, flags, and profile names (including profiles you install
later) never go stale.

Install:
  bash: source <(bulle completion bash)   in ~/.bashrc
  zsh:  source <(bulle completion zsh)    in ~/.zshrc (after compinit)
  fish: bulle completion fish > ~/.config/fish/completions/bulle.fish
`,

	"help": `Show help.

Usage:
  bulle help [subcommand or topic]

Without an argument, prints the main help. With a subcommand name, prints
that subcommand's help — the same text as bulle <subcommand> --help. The
extra topics hold the advanced material: grants, env, limits, config.
`,

	"version": `Show version information.

Usage:
  bulle version
`,
}

var helpTopics = map[string]string{
	"grants": `Path grants, in flags and profiles.

Grant flags (repeatable, comma-separated; executables must be allowed
explicitly with --rox or --rwx):
  --ro PATH        read-only access
  --rox PATH       read-only access plus execute
  --rw PATH        read-write access
  --rwx PATH       read-write access plus execute
  --no-workspace   do not automatically grant the workspace read-write access

Path entries (the same syntax works in profile TOML lists):
  ?path            optional: skip silently when the path does not exist
  +path            create an empty file when missing (rw/rwx entries)
  +path/           create a directory when missing (rw/rwx entries)
  which:NAME       resolve NAME on the parent PATH and grant that binary only,
                   findable by name inside the sandbox (rox/rwx entries)
  pkg:NAME         like which:, plus the tool's package tree (rox/rwx entries)
  TOOL:ASPECT      ask a tool where its own directories are, such as r:libs or
                   uv:cache; valid in every list (see bulle show resolvers)
  dir/*/bin        single-star glob; no matches means the entry is skipped

Variables: $WORKSPACE, $HOME, $TMP, $TMPDIR, and the per-platform $CONFIG,
$DATA, $CACHE, $STATE (XDG dirs on Linux, ~/Library on macOS).
${NAME:-fallback} falls back when NAME is unset. Define your own with
--var NAME=VALUE (usable in grants as $NAME).

A command given explicitly after -- always gets its own binary granted, plus
its runtime libraries (discovered precisely from ELF dependencies on Linux;
from configured runtime roots on macOS).
`,

	"env": `Environment variables inside the sandbox.

No variables are passed unless requested:
  --env NAME        pass NAME from the current environment, if it is set
  --env NAME=VALUE  set NAME to VALUE inside the sandbox
  --env 'GLOB'      pass every parent variable matching a name glob (GIT_*)
  --env-file PATH   load NAME=VALUE entries from a dotenv-style file
  --env-all-except NAME,...
                    pass the whole parent environment except the named variables

Profiles carry an env list with the same NAME / NAME=VALUE / glob syntax.
`,

	"limits": `Timeouts and resource limits.

  --timeout DURATION
                    kill the whole sandboxed process tree if it runs longer
                    than DURATION.
                    Go duration syntax such as 30s, 2m, or 1h30m; 0 disables.
                    Linux requires a delegated cgroup; macOS has no secure
                    unprivileged process-tree primitive, so nonzero timeouts
                    fail before exec there. Timed-out commands exit 124.
  --memory SIZE     cap resident memory, as in 512M or 4G
  --cpu PERCENT     cap CPU use as a percentage of one core, as in 200%
  --nproc N         cap the number of processes in the sandbox
  --nofile N        cap the number of open file descriptors
  --fsize SIZE      cap the size of any single file written, as in 100M
  --cpu-time DURATION
                    cap consumed CPU time, as opposed to the wall clock
                    measured by --timeout. An idle process burns wall clock
                    but no CPU, so the two catch different runaways. Applies
                    per process, not to the whole tree: each child gets its
                    own budget.
  --strict-limits   refuse to run when a limit cannot be enforced here,
                    instead of warning and continuing

--memory, --cpu, and --nproc need cgroup v2, so they apply on Linux with a
delegated cgroup and nowhere else; macOS has no per-process-tree equivalent
for any of the three. The others are portable. bulle warns on stderr about
any limit it cannot enforce, and bulle show policy names the mechanism behind
each one. Set a limit under [defaults.linux] to request it only where it
works.
`,

	"config": `Configuration files and defaults.

Configuration lives in a directory (default: the platform config dir, e.g.
~/.config/bulle on Linux):
  config.toml       machine-local settings, [vars], and the [defaults] block
  profiles/*.toml   one profile per file, named after the file

A profile file named after an existing profile merges into it, adding its
grants to that profile everywhere the profile is used. That is where the
entries bulle prints after a denied run belong.

Related flags:
  --config PATH     use this configuration directory
  --var NAME=VALUE  define a custom path variable usable in grants as $NAME
  --no-defaults     ignore the [defaults] block of the user configuration

bulle show config reports what is in effect on this machine.
`,
}
