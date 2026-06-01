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
bulle /path/to/project --profile claude
```

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

Instead of specifying the path of every command manually, we can load [profiles](#profiles): named bundles of permissions for common tools. `bulle` ships with built-in profiles for several coding agents. For example, the first command below gives read-write access to the current directory, and launches Claude Code with minimal permissions:

```sh
bulle --profile claude
# bulle /path/to/project --profile claude
# bulle --profile codex
# bulle --profile pi
# bulle --profile opencode
```

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

## Environment

Environment variables are also explicit. By default, `bulle` does not pass your shell environment into the sandbox. Use `--env NAME` to pass a variable from the parent environment, or `--env NAME=VALUE` to define one on the fly:

```bash
bulle --rox /usr/bin --env HELLO=WORLD -- printenv HELLO
```

This is important for secrets. A command cannot read `OPENAI_API_KEY`, `GITHUB_TOKEN`, or similar variables unless you explicitly pass them.

The summary and JSON views list environment variables by name only; neither view prints their values.

## Profiles

A profile is a named bundle of path, environment, network, and platform grants. It saves you from spelling out the same permissions every time you run a tool.

!!! warning

    Profiles can grant broad filesystem, environment, network, and platform access. Use `--policy` to inspect the resolved permissions before running an unfamiliar profile or combining profiles.

### Use

The simplest way to use `bulle` is to select a built-in agent profile. This will launch the Claude Code app with appropriate permissions and constraints:

```bash
bulle --profile claude
```

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
keychain
macos-certs
macos-dns
network
offline
opencode
pi
tool
```

Built-in helper profiles such as `default`, `network`, `offline`, `macos-dns`, `macos-certs`, and `keychain` are ordinary profiles that can be inherited directly or selected explicitly when you pass a command.

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

Path grants can use placeholders. `$WORKSPACE` refers to the workspace path. Fixed placeholders are `$HOME`, `$WORKSPACE`, `$TMP`, and `$TMPDIR`; custom path variables are not part of the config model.

`inherits` can be one profile name or an array of profile names. Parents are merged left to right and the child is applied last. Path grants merge by path and promote permissions, so `rox` plus `rw` for the same path becomes `rwx`. Environment entries merge by variable name with later values winning. Network and Mach allow/deny entries supersede by name.

`env` entries can be variable names copied from the parent environment or explicit `KEY=value` assignments. The only current network capability name is `network`, so `allow = ["network"]` enables network access and `deny = ["network"]` disables it.

The `[macos]` and `[linux]` tables are applied only on that platform. They accept `default_app`, path grants, `env`, `allow`, `deny`, `mach_lookup`, `deny_mach_lookup`, `add_exec`, and `add_libs`. They do not accept profile metadata or `inherits`.

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
