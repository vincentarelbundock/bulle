package cli

// CommandHelp returns focused help for one subcommand, printed for
// `bulle help <name>` and `bulle <name> --help`. The bool reports whether the
// topic exists; app tests assert every dispatched subcommand has an entry, so
// the topics cannot drift from the dispatch table.
func CommandHelp(name string) (string, bool) {
	text, ok := commandHelp[name]
	return text, ok
}

var commandHelp = map[string]string{
	"policy": `Resolve and print the sandbox policy without running anything.

Usage:
  bulle policy [run flags] [workspace] [-- command [args...]]

Flags:
  --json            print the resolved policy as JSON
  --summary         print a human-readable summary (default)

policy accepts every run flag, so it shows exactly what a run with the same
arguments would grant. Without a command, command-dependent grants (add_exec,
shebang discovery) are simply absent from the printed policy.

Examples:
  bulle policy --profile codex
  bulle policy --json . --rox /bin -- /bin/bash
`,

	"rerun": `Repeat the previous invocation, with extra flags inserted.

Usage:
  bulle rerun [extra flags]

Every run records its arguments; rerun repeats the last one from the same
directory, inserting any extra flags before the command. Typical after a
denial hint:

  bulle rerun --ro ~/.gitconfig
`,

	"scratch": `Run in a disposable clone of the workspace, or manage kept scratches.

Usage:
  bulle scratch [run flags] [workspace] [-- command [args...]]
  bulle scratch list|diff|pull|wipe|shell [id]

A scratch is a disposable local clone of a git workspace (uncommitted work is
carried over). After the run, review the changes: diff, pull into the origin,
keep, open a shell in the scratch, or wipe it. Everything after "scratch"
that is not a review verb is an ordinary run; use -- to run a command named
like a verb.

The review verbs resume a kept scratch later; id may be a unique prefix, and
is optional when unambiguous.

Related run flags:
  --scratch         run in a scratch, as a flag — for composing with other
                    subcommands, as in bulle rerun --scratch
  --scratch-keep    skip the review prompt, keep the scratch, print its path
`,

	"record": `Draft a profile from a real run by collecting sandbox denials.

Usage:
  bulle record --profile NAME [--out FILE] [--max-rounds N] -- command [args...]

Runs the command under profile NAME, collects what the sandbox denied, adds
those grants, and runs again until nothing new is denied. Prints a profile
inheriting from NAME that contains only the additions.

Flags:
  -o, --out FILE    write the profile to FILE instead of stdout
  --max-rounds N    stop after N rounds (default 10); reaching the cap with
                    the command still failing is reported, and the profile
                    says so

A recording is evidence that one run of one command needed these grants — not
that they are sufficient. A denial aborts the operation that hit it, so
anything the command did not reach is missing. Review before installing.

On macOS the unified log reports violations from every sandboxed process, so
entries may not all be yours; each one names the process it was denied to.
`,

	"profiles": `List available profiles or install new ones.

Usage:
  bulle profiles list
  bulle profiles install SOURCE

install accepts profile TOML files from a file, a directory, a local git
repository, or a GitHub source (github:owner/repo[/subdir]), and copies them
into your configuration directory's profiles/ folder.

Examples:
  bulle profiles install agent.toml
  bulle profiles install github:vincentarelbundock/bulle/custom_profiles
`,

	"resolvers": `List path resolvers and what they resolve to on this machine.

Usage:
  bulle resolvers

Resolvers are the TOOL:ASPECT path entries usable in profiles and grant
flags — for example r:libs or uv:cache — which ask a tool where its own
directories are.
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
  bulle help [subcommand]

Without an argument, prints the full help. With a subcommand name, prints
that subcommand's help — the same text as bulle <subcommand> --help.
`,

	"version": `Show version information.

Usage:
  bulle version
`,
}
