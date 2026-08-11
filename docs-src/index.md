---
title: ""
description: A simple sandbox for dangerous tools like coding agents
hide:
  - title
  - navigation
---

<p align="center"><img src="assets/bulle.svg" alt="bulle logo" width="300"></p>

<p align="center" style="font-size: 1.2em;"><strong>A simple sandbox for dangerous tools like coding agents</strong></p>

`bulle` is an easy-to-use sandbox for running local commands while exposing only the essential parts of your machine. It allows you to run tools you don't fully trust, without handing over all your files or secrets, and with an option to deny network access. `bulle` sandboxes are especially helpful when running LLM coding agents or untrusted scripts.

You can spin up an agent with restricted permissions using this simple command:

```bash
bulle /path/to/project -- claude
```

`bulle` notices that the built-in `claude` profile is designed to run this command, announces the profile it selected, and applies its permissions automatically. The same works for `codex`, `opencode`, and `pi`.

Sandboxes are not limited to agents. You can use `bulle` to run any command with custom permissions. See the [Quick start](#quick-start) section and the [CLI reference](cli-reference) for details.

!!! warning "`bulle` is still experimental. Please report bugs, comments, and feature requests [on GitHub](https://github.com/vincentarelbundock/bulle)."

## Risk Mitigation

`bulle` uses [Operating System-level sandboxing](#os-level-sandboxing) to constrain a command's access to paths and environment variables. Like all sandboxing approaches, this strategy imposes trade-offs between convenience and safety. `bulle` will not solve all your security problems, but it can mitigate some important risks.

!!! success "`bulle` can mitigate risk when"

    - a prompt or skill injection tells an agent to steal passwords or keys stored outside the sandbox;
    - an LLM agent or script tries to rewrite `~/Documents` instead of the project where it should be running;
    - a malicious package searches your home directory for cloud credentials;
    - a crash log exposes your `API_KEY` environment variable;
    - a tool surreptitiously runs code from downloads, caches, or another project.

!!! warning "`bulle` is not sufficient when"

    - the command needs network access but should not send readable code to a specific service;
    - the command itself needs secrets or paths you cannot afford to leak;
    - you need CPU, memory, disk, or time limits;
    - you are running code from hostile parties and need a separate machine boundary, not just local OS rules.

For more information on sandboxing tradeoffs, read [A field guide to sandboxes for AI](https://www.luiscardoso.dev/blog/sandboxes-for-ai) by Luis Cardoso.

## Install

`bulle` is only available on MacOS and Linux.

With the install script:

```sh
curl -fsSL https://raw.githubusercontent.com/vincentarelbundock/bulle/main/install.sh | sh
```

With Homebrew:

```sh
brew install vincentarelbundock/tap/bulle
```

Or download a prebuilt `darwin`/`linux`, `amd64`/`arm64` archive from the [latest GitHub release](https://github.com/vincentarelbundock/bulle/releases/latest).

## Quick Start

By default, `bulle` runs in the current directory. Access to any other location in the filesystem is denied unless you grant it explicitly. Commands cannot read files, execute programs, or inherit environment variables unless you allow them.

```bash
bulle -- ls
```

```text
command not found before sandbox setup: "ls"
Grant an executable path with --rox/--rwx, choose a profile,
or pass an explicit executable path after --
```

That error is intentional: even finding and executing `ls` requires permission. Grant read-and-execute access to a directory with `--rox`, and `bulle` can find commands in it:

```bash
bulle --rox /bin -- ls
```

Instead of specifying the path of every command manually, we can use [profiles](#profiles): named bundles of permissions for common tools. `bulle` ships with built-in profiles for several coding agents, and when your command matches the tool a profile was made for, `bulle` selects that profile for you. The commands below each give read-write access to a workspace and launch an agent with minimal permissions:

```sh
bulle -- claude
# bulle /path/to/project -- claude
# bulle -- codex
# bulle -- pi
# bulle -- opencode
```

`bulle` announces the choice on startup, for example:

```text
bulle: selected profile "claude" because its default_app runs "claude" and
the default profile cannot; pass --profile to choose explicitly
```

You can always pick a profile explicitly instead — `bulle --profile claude`, which also launches the app for you — or combine profiles and one-off grants; see [Profiles](#profiles) for the distinction between selecting profiles and letting `bulle` infer one.

## Filesystem

The workspace is the command's working directory and writable area. If omitted, it defaults to the current directory. Use `--no-workspace` when you do not want this automatic read-write grant.

Additional filesystem access is explicit. Use these flags to add paths to the active policy:

```bash
--ro path        # read-only
--rox path       # read-only plus execute
--rw path        # read-write
--rwx path       # read-write plus execute
--no-workspace   # do not automatically grant the workspace read-write access
```

!!! note

    Grant the narrowest paths that are practical. Use `--rw` or `--rwx` only for paths outside the workspace that the command should be allowed to modify.

The `--` separator may be omitted when the reading is unambiguous. The first positional argument that is an existing directory reads as the workspace; the first positional that is not an existing directory starts the command, and `bulle` announces the split on stderr:

```bash
bulle ~/repos/project git status
# bulle: treating "git" as the start of the command; use -- to separate the command explicitly
```

Ambiguity resolves toward the workspace reading, so `--` remains the explicit way to force a directory name to be treated as a command.

## Environment

Environment variables are also explicit. By default, `bulle` does not pass your shell environment into the sandbox. Use `--env NAME` to pass a variable from the parent environment, or `--env NAME=VALUE` to define one on the fly:

```bash
bulle --rox /usr/bin --env HELLO=WORLD -- printenv HELLO
```

This is important for secrets. A command cannot read `OPENAI_API_KEY`, `GITHUB_TOKEN`, or similar variables unless you explicitly pass them.

Three conveniences cover common cases:

- **Name globs.** `--env 'GIT_*'` passes every parent variable whose name matches the glob. Quote the pattern so your shell does not expand it. Globs also work in profile `env` lists.
- **Dotenv files.** `--env-file PATH` loads `NAME=VALUE` entries from a dotenv-style file: blank lines and `#` comments are ignored, an optional `export ` prefix is stripped, and matching single or double quotes around values are removed.
- **Everything except.** `--env-all-except SECRET_KEY,GITHUB_TOKEN` passes the whole parent environment minus the named variables, for throwaway commands that want your shell environment without specific secrets.

When sources conflict, the most explicit wins: profile entries, then `--env-all-except`, then `--env-file`, then `--env`.

The summary and JSON views list environment variables by name only; neither view prints their values.

## Profiles

A profile is a named bundle of path, environment, network, and platform grants. It saves you from spelling out the same permissions every time you run a tool.

!!! warning

    Profiles can grant broad filesystem, environment, network, and platform access. Use `--policy` to inspect the resolved permissions before running an unfamiliar profile or combining profiles.

### Selection and inference

There are two ways a profile gets applied to a run.

**Explicit selection** with `--profile` names the profile directly. When the profile declares a `default_app`, you do not even need to pass a command — this launches Claude Code with appropriate permissions and constraints:

```bash
bulle --profile claude
```

**Inference** goes the other way: you pass the command, and `bulle` finds the profile made for it. When no `--profile` is given and the command cannot run under the default profile, `bulle` checks whether exactly one installed profile declares that command as its `default_app`. If so, it selects that profile, announces the choice, and continues:

```bash
bulle -- claude
```

```text
bulle: selected profile "claude" because its default_app runs "claude" and
the default profile cannot; pass --profile to choose explicitly
```

Inference is deliberately conservative, because applying a profile changes what the sandbox grants:

- It only ever rescues a run that would otherwise fail command discovery. A command that already works under the default profile is never re-profiled.
- It never fires when you pass `--profile` — an explicit selection always wins.
- If several profiles declare the same command, `bulle` refuses to guess, lists the candidates, and asks you to choose with `--profile`.
- The selection is always announced on stderr, and `--policy` shows the resulting permissions (`bulle --policy -- claude`).

Without a profile or an explicit grant, `bulle` cannot find or execute anything, so command discovery fails before the sandbox starts:

```text
bulle -- ping google.com

command not found before sandbox setup: "ping" is not on policy PATH, parent PATH, or executable roots. Add --env PATH with matching --rox/--rwx roots, add a --rox/--rwx root containing the command, choose a profile, or pass an explicit executable path after --
```

The built-in `tool` profile adds `PATH`, executable discovery, temporary directory access, runtime library access, and network access:

```text
bulle --profile tool -- ping google.com

PING google.com (...): 56 data bytes
64 bytes from ...
```

Comma-separated profiles merge from left to right. Adding `offline` after `tool` keeps the command setup but removes network access:

```text
bulle --profile tool,offline -- ping google.com

ping: cannot resolve google.com: Unknown host
```

You can still add one-off permissions on top of a profile:

```text
bulle --profile claude --ro README.qmd --rw ~/Desktop --env GITHUB_TOKEN
```

### List

Use `--list-profiles` to print available profiles:

```bash
bulle --list-profiles

claude
codex
default
git
go
keychain
macos-certs
macos-dns
network
node
offline
opencode
pi
rust
terminal
tool
```

Built-in helper profiles such as `default`, `network`, `offline`, `macos-dns`, `macos-certs`, and `keychain` are ordinary profiles that can be inherited directly or selected explicitly when you pass a command.

### Capability micro-profiles

The built-in `git`, `node`, `rust`, `go`, and `terminal` profiles are small capability bundles, each answering one concern once and portably. The tool profiles use `which:`/`pkg:` resolvers, tool resolvers such as `npm:cache` and `go:modcache`, platform variables, and optional/create markers to settle a tool's location questions (binary, configuration, caches); `terminal` carries the environment variables an interactive program expects. They are meant to be assembled through `inherits` rather than restating tool layouts in every agent profile:

```toml
title = "My agent"
inherits = ["tool", "git", "node"]
default_app = "my-agent"
```

They can also be combined on the command line: `bulle --profile tool,git,rust -- cargo build`.

### Install

Install or override profiles with `--install-profiles SOURCE`. The source can be one `.toml` file, a directory containing `.toml` files, a local git repository, or a GitHub source such as `github:vincentarelbundock/bulle/custom_profiles`.

```bash
bulle --install-profiles agent.toml
bulle --install-profiles ./profiles
bulle --install-profiles github:vincentarelbundock/bulle/custom_profiles
```

By default, profiles are installed under the operating system user config directory: usually `$XDG_CONFIG_HOME/bulle/profiles/` or `~/.config/bulle/profiles/` on Linux, and `~/Library/Application Support/bulle/profiles/` on macOS. Use `--config PATH` to install into a different config directory; `bulle` creates its `profiles/` subdirectory if needed. The filename becomes the profile name, so `profiles/agent.toml` is selected with `--profile agent`.

When installing from a local git repository root or `github:owner/repo`, `bulle` uses `profiles/*.toml` if that directory exists. When a GitHub source includes a subdirectory, such as `github:owner/repo/custom_profiles`, that subdirectory is used as the profile source.

### TOML

Built-in and user profiles use the same one-profile TOML format. The filename is the profile name, and profile fields live at the top level of that file. This example shows all profile option groups:

```toml
title = "Agent"
description = "custom Codex profile"
inherits = ["tool", "keychain"]
default_app = "codex"

ro = ["README.md"]
rox = ["/usr/bin"]
rw = ["$TMP/bulle/tmp"]
rwx = ["$HOME/.cache/example-agent"]

env = ["HOME", "USER", "NODE_ENV=development"]
allow = ["network"]
deny = []

add_exec = true
add_libs = true

[macos]
ro = ["$HOME/Library/Preferences"]
mach_lookup = ["com.apple.trustd.agent"]
deny_mach_lookup = ["com.apple.SystemConfiguration.configd"]

[linux]
ro = ["$HOME/.config"]
rox = ["/usr/bin"]
```

Available top-level options are `title`, `description`, `inherits`, `default_app`, path grants (`ro`, `rox`, `rw`, `rwx`), `env`, network settings (`allow`, `deny`), macOS Mach services (`mach_lookup`, `deny_mach_lookup`), executable discovery defaults (`add_exec`, `add_libs`), and platform tables (`[macos]`, `[linux]`). Only `title` and `description` are metadata fields.

Path grants can use variables and entry markers; see [Portable profiles](#portable-profiles) below.

`inherits` can be one profile name or an array of profile names. Parents are merged left to right and the child is applied last. Path grants merge by path and promote permissions, so `rox` plus `rw` for the same path becomes `rwx`. Environment entries merge by variable name with later values winning. Network and Mach allow/deny entries supersede by name.

`env` entries can be variable names copied from the parent environment or explicit `KEY=value` assignments. The only current network capability name is `network`, so `allow = ["network"]` enables network access and `deny = ["network"]` disables it.

The `[macos]` and `[linux]` tables are applied only on that platform. They accept `default_app`, path grants, `env`, `allow`, `deny`, `mach_lookup`, `deny_mach_lookup`, `add_exec`, and `add_libs`. They do not accept profile metadata or `inherits`.

### Portable profiles

A profile written on one machine should work on another, even when tools are installed in different places. Several features make path entries portable.

**Path variables.** `$WORKSPACE` is the workspace path; `$HOME`, `$TMP`, and `$TMPDIR` are fixed. Four variables resolve per platform, so one entry covers both operating systems:

| Variable  | Linux                               | macOS                           |
|-----------|-------------------------------------|---------------------------------|
| `$CONFIG` | `$XDG_CONFIG_HOME` or `~/.config`   | `~/Library/Application Support` |
| `$DATA`   | `$XDG_DATA_HOME` or `~/.local/share`| `~/Library/Application Support` |
| `$CACHE`  | `$XDG_CACHE_HOME` or `~/.cache`     | `~/Library/Caches`              |
| `$STATE`  | `$XDG_STATE_HOME` or `~/.local/state`| `~/Library/Application Support` |

On Linux, the raw `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, and `XDG_STATE_HOME` names are also available and honor the parent environment when set. On macOS, several of these collapse to the same directory; the `--policy` resolution table flags entries whose grants merge because of that.

**Optional and created entries.** Read-only entries (`ro`, `rox`) are always skipped silently when the path does not exist. Writable entries (`rw`, `rwx`) fail hard on a missing path unless marked:

```toml
rw  = ["?$HOME/.netrc"]        # ? optional: skip when missing
rwx = ["+$HOME/.codex/"]       # + trailing slash: create the directory when missing
rw  = ["+$HOME/.claude.json"]  # + no slash: create an empty file when missing
```

Creation is the right default for a tool's own state: silently skipping a missing writable grant would produce a mysterious permission error later, and the built-in agent profiles use `+` for their state directories so they work on a fresh machine.

**Executable resolvers.** `which:NAME` looks up `NAME` on the parent `PATH` at policy-build time and grants exactly that binary (and its symlink chain) — never its containing directory, so `which:codex` does not hand over everything else in the same `bin`. `pkg:NAME` additionally grants the tool's package tree (two levels above the real binary), which Node- and Python-based tools need for their libraries; it refuses to expand when that tree would be a system directory such as `/usr`. Both are valid only in `rox` and `rwx`:

```toml
rox = ["which:git", "which:rg", "pkg:codex"]
```

Name lookup inside the sandbox works through a per-run shim directory of symlinks that `bulle` creates outside the sandbox's writable area, grants read+execute, prepends to `PATH`, and removes after the run. A `which:`-based profile must name every binary the tool shells out to; missing ones surface through denial diagnostics. The broad-grant `tool` profile remains the escape hatch.

**Tool resolvers.** Some layouts cannot be written down at all. R's package search path holds one entry per installed package — 70 of them on a Nix machine, each a separate store path, changing on every update. Instead of guessing, ask the tool: an entry of the form `TOOL:ASPECT` runs a known query before the sandbox starts and grants whatever it reports.

```toml
rox = ["r:home", "r:prefix"]
ro  = ["r:libs"]
rw  = ["?+npm:cache", "?+go:modcache"]
```

Unlike `which:`/`pkg:`, these name ordinary directories and are valid in every list. Markers work as usual: `?` skips when the tool is absent, `+` creates a directory the tool reports but has not made yet. Results are re-resolved as literal paths, so the symlink pairing and sensitive-target refusals apply exactly as they do to a hand-written path.

Run `bulle --list-resolvers` to see every resolver and what it points at on the current machine — a resolver that reports nothing is otherwise indistinguishable from one that works. The set is fixed by `bulle`: a profile chooses from it but can never supply a command to run, because profiles are installable from GitHub and one that could run commands would make installing a profile equivalent to running its author's code. An unknown namespace is an error rather than a literal path, and a literal path in that shape must be written `./ruby:gems` or absolute.

Some aspects are guarded. `r:prefix` reports R's installation root, which is narrow where R owns its own directory (a Nix store path, a Homebrew Cellar entry) and far too broad where it is `/usr`; results matching the `pkg:` system-root denylist are dropped rather than granted.

**Globs.** A `*` matches within one path segment (no `**`). No matches means the entry is skipped, so version-stamped directories stop breaking on upgrade:

```toml
rox = ["$HOME/.nvm/versions/node/*/bin"]
```

**Custom variables.** Machine-specific layouts live in a `[vars]` table in `<config>/config.toml` (usually `~/.config/bulle/config.toml`), or one-off with `--var NAME=VALUE`:

```toml
[vars]
PROJECTS = "/mnt/work/repos"
```

Profiles then reference `$PROJECTS`, and `${NAME:-fallback}` supplies a default when a variable is unset. A small allowlist of well-known tool environment variables (`CARGO_HOME`, `GOPATH`, `NVM_DIR`, `PYENV_ROOT`, and similar) may also be referenced; their values come from the parent environment and are treated as untrusted — values that are not absolute paths, or that resolve to `/` or the home directory, are ignored so a hostile environment cannot widen a grant. Custom variable names are uppercase, and reserved names (`HOME`, `WORKSPACE`, `TMP`, `CONFIG`, `DATA`, `CACHE`, `STATE`, `XDG_*`, ...) cannot be redefined.

## Rerunning With Added Grants

A sandboxed run often fails because one grant is missing. The kernel-level [denial diagnostics](denial-diagnostics) already print the missing grants after a failed run; `bulle` now also prints a copy-pasteable retry line:

```text
bulle: the sandbox denied the following accesses during this run:
  denied: read /home/user/.gitconfig — add --ro ~/.gitconfig
bulle: retry with these grants: bulle --last --ro ~/.gitconfig
```

`bulle --last` repeats the previous invocation — from any shell, restoring the original working directory — and inserts any extra flags before the command, so the retry line works as-is. Each run overwrites the recorded invocation, and repeated `--last` runs accumulate their added grants. The sandbox is restarted rather than widened: Landlock cannot extend a live sandbox, and agents resume from their own session state.

The invocation is recorded in `$XDG_STATE_HOME/bulle/last-run.json` (usually `~/.local/state/bulle/`) on Linux and `~/Library/Application Support/bulle/` on macOS.

## Configuration Defaults

A `[defaults]` block in `<config>/config.toml` (usually `~/.config/bulle/config.toml`) supplies values used when the corresponding flag is absent, so bare `bulle` does the usual thing in a repository:

```toml
[defaults]
profile = "claude"
timeout = "2h"
env = ["GITHUB_TOKEN"]
ro = ["?~/.gitconfig"]
```

Explicit flags always win: `--profile codex` overrides the default profile, `--timeout` the default timeout, and list-valued defaults (`env`, `ro`, `rox`, `rw`, `rwx`) are merged with command-line entries taking precedence. Pass `--no-defaults` to ignore the block entirely.

## Network

Network access is controlled by profiles. The built-in `network` profile allows it, and the built-in `offline` profile denies it. On macOS, the `network` profile also inherits DNS and certificate service bundles that network clients normally need. Built-in agent profiles inherit network access for compatibility with package managers and remote services.

```bash
bulle --profile offline --rox /bin -- /bin/ls
bulle --profile codex,offline
```

## Policy

Use `--policy` to inspect the resolved sandbox policy without running the command. By default, it prints the same human-readable permissions summary that `bulle` sends to supported LLM agent profiles at startup. This is a useful safety check before launching an agent or script, especially when combining profiles with extra filesystem or environment grants.

```bash
bulle --profile codex --policy
```

The summary ends with a **resolution table**: one line per configured entry showing what it resolved to — granted, skipped, created, or expanded from a `which:`/`pkg:` resolver or glob — so "why can't the agent see X" is answerable from one command. Entries whose resolved path is also granted through another list (for example when `$CONFIG` and `$DATA` collapse to the same directory on macOS) are flagged with their effective permission.

```text
  resolution:
    rox  which:codex        → /home/user/.local/share/mise/installs/node/22/bin/codex (+1 more)
    rwx  +$HOME/.codex/     → created (dir) /home/user/.codex
    rw   ?$HOME/.netrc      → skipped (does not exist)
```

Stable machine-readable output is available with `--policy=json`:

```bash
bulle --policy=json ~/Desktop --rox /bin -- /bin/ls
```

```json
{
  "backend": "macos-seatbelt",
  "workspace_path": "/home/user/Desktop",
  "command": ["/bin/ls"],
  "ro": [],
  "rox": ["/bin"],
  "rw": ["/home/user/Desktop"],
  "rwx": [],
  "env_keys": [],
  "add_exec": false,
  "add_libs": false,
  "mach_lookup": [],
  "network": "full"
}
```

In the `--policy=json` example, `workspace_path` is the directory where the command would run. Because workspaces are granted automatically by default, the command would run with read-write access to `/home/user/Desktop`, shown in the `rw` array. The `command` field is the command that would be executed, and the `ro`, `rox`, `rw`, and `rwx` arrays show the readable, executable, writable, and writable-executable path grants. The `env_keys` array lists environment variables that would be passed into the sandbox. The `mach_lookup` array lists configured macOS Mach services. The `network` field shows the resolved network state. The `backend` value depends on your operating system.

## Executables and Libraries

For quick local commands, `--add-exec` can save you from spelling out executable grants by hand. It resolves the command before the sandbox starts and adds the executable to the policy:

```bash
bulle --add-exec -- /bin/ls
```

On Linux, dynamically linked executables also need access to runtime libraries. `--add-libs` discovers the shared libraries needed by the executable and adds read-only grants for them:

```bash
bulle --add-exec --add-libs -- /usr/bin/git status
```

These flags are conveniences for executables and runtime libraries. They do not add app state files, config directories, caches, secrets, or shell environment variables. Use profiles for agents and other tools that need a larger, repeatable policy.

Profiles can enable these conveniences with `add_exec = true` and `add_libs = true`. Boolean settings inherit like other scalar profile settings: an explicit value in a later inherited profile or child profile overrides the earlier value.

## OS-Level Sandboxing

`bulle` builds a policy before the command starts. The policy is assembled from the workspace, selected profile, command-line flags, selected environment variables, network profile settings, executable discovery, and runtime library defaults. Paths are resolved before sandbox setup, and `--policy` prints the resulting policy without running the command.

### Linux

On Linux, `bulle` applies the policy with [Landlock](https://docs.kernel.org/userspace-api/landlock.html). Landlock is a kernel feature, not a package to install; basic filesystem sandboxing requires Linux 5.13 or later with Landlock enabled. The Linux backend restricts filesystem access for the process and its children according to the resolved read, write, and execute grants. When the resolved network setting is denied, it also installs a seccomp filter before `exec` to deny socket-related system calls.

### macOS

On macOS, `bulle` generates a [Seatbelt](https://www.unix.com/man_page/osx/5/sandbox/) profile and runs the command with `/usr/bin/sandbox-exec`. The macOS backend maps the same policy model to Seatbelt rules, including filesystem rules, optional network allowance, and selected Mach service access from configured `mach_lookup` entries. This is useful for local workflows, but its behavior is not identical to Linux Landlock.

## License and Attribution

`bulle` is distributed under the MIT License. See [LICENSES/bulle-MIT.txt](LICENSES/bulle-MIT.txt).

Thank you to [Landrun](https://github.com/Zouuup/landrun), an excellent, compact Go implementation of practical Landlock sandboxing. The Linux sandbox backend and filesystem permission model owe a clear debt to Landrun's design, and portions of the Linux backend and ELF dependency discovery are derived from or inspired by Landrun. See [LICENSES/landrun-MIT.txt](LICENSES/landrun-MIT.txt) for the full third-party notice and license.
