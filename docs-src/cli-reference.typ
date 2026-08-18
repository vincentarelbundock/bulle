#set document(title: [CLI Reference])
#metadata((
  title: "CLI Reference",
  description: "Command-line reference for bulle.",
)) <website-metadata>

#title()

This page is generated from bulle --help.

````text
bulle runs coding agents and other dangerous tools inside a sandbox.

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

When a run is denied something, bulle ends by printing the profile entries
that would have allowed it, for you to add to the profile yourself.

Subcommands:
  bulle scratch <profile> [dir] [-- command]   run in a disposable clone
  bulle scratch list|diff|pull|wipe|shell      review kept scratches
  bulle show [policy|profiles|resolvers|config]
                                               inspect without running
  bulle profiles install [--force] SOURCE      install profiles (file, dir, git, github:)
  bulle completion bash|zsh|fish               shell completion
  bulle help [grants|env|limits|config]        the advanced material

Run "bulle help grants" for path syntax (?, +, which:, pkg:, resolvers,
variables), "bulle help env" for env files and globs, "bulle help limits" for
timeouts and resource caps, "bulle help config" for configuration, and
"bulle show profiles" for the profiles available on this machine.
````
