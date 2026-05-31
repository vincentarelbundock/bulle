---
title: ""
description: A simple sandbox for dangerous tools like coding agents
hide:
  - title
  - navigation
---

<p align="center"><img src="assets/bulle.svg" alt="bulle logo" width="300"></p>

<p align="center" style="font-size: 1.2em;"><strong>A simple sandbox for dangerous tools like coding agents</strong></p>

`bulle` is an easy-to-use sandbox for running local commands while exposing only the essential parts of your machine. It allows you to run tools you don't fully trust, without handing over all your files or every secret held in environment variables. `bulle` sandboxes are especially helpful when running LLM coding agents or untrusted scripts.

On macOS and Linux, you can spin up an agent with restricted permissions using this simple command:

```bash
bulle /path/to/project --profile claude
```

Sandboxes are not limited to agents. You can use `bulle` to run any command with custom permissions. See the [Quick start](#quick-start) section and the [CLI reference](cli-reference) for details.

!!! warning "`bulle` is still experimental. Please report bugs, comments, and feature requests [on GitHub](https://github.com/vincentarelbundock/bulle)."

## Risk mitigation

`bulle` limits commands to the files and variables you explicitly allow.

!!! success "`bulle` can mitigate risk when"

    - a prompt or skill injection tells an agent to steal your passwords or SSH keys;
    - an LLM or script tries to rewrite `~/Documents` instead of the project where it should be running;
    - a malicious package searches your home directory for cloud credentials;
    - a crash log exposes your `API_KEY` environment variable;
    - a tool surreptitiously runs code from downloads, caches, or another project.

!!! warning "`bulle` is not sufficient when"

    - a command should not send readable code over the network;
    - the command itself needs secrets or paths you cannot afford to leak;
    - you need CPU, memory, disk, or time limits;
    - you are running code from hostile parties and need a separate machine boundary, not just local OS rules.

For more information on sandboxing tradeoffs, read [A field guide to sandboxes for AI](https://www.luiscardoso.dev/blog/sandboxes-for-ai) by Luis Cardoso.

## Install

With the install script:

```sh
curl -fsSL https://raw.githubusercontent.com/vincentarelbundock/bulle/main/install.sh | sh
```

To install somewhere other than `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/vincentarelbundock/bulle/main/install.sh | sh -s -- -b ~/.local/bin
```

With Homebrew:

```sh
brew install vincentarelbundock/tap/bulle
```

With Go:

```sh
go install github.com/vincentarelbundock/bulle/cmd/bulle@latest
```

Or download a prebuilt `darwin`/`linux`, `amd64`/`arm64` archive from the [latest GitHub release](https://github.com/vincentarelbundock/bulle/releases/latest).

## Quick start

By default, `bulle` runs in the current directory. Access to any other location in the filesystem is denied unless you grant it explicitly. Commands cannot read files, execute programs, or inherit environment variables unless you allow them.

```bash
bulle -- ls
```

```text
command not found before sandbox setup: "ls" is not on policy PATH.
Add --env PATH with matching --rox/--rwx roots, choose a profile,
or pass an explicit executable path after --
```

That error is intentional: even finding and executing `ls` requires permission. We can grant that permission using the `--rox` flag (read+execute):

```bash
bulle --rox /bin/ls -- /bin/ls
```

Instead of specifying the path of every command manually, we can load [profiles](#profiles): named bundles of permissions for common tools. `bulle` includes profiles for several coding agents. For example, the first command below gives read-write access to the current directory, and launches Claude Code with appropriate permissions:

```sh
bulle --profile claude
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

Advice: grant the narrowest paths that are practical. Use `--rw` or `--rwx` only for paths outside the workspace that the command should be allowed to modify. In profiles, `$WORKSPACE` refers to the workspace path.

## Environment

Environment variables are also explicit. By default, `bulle` does not pass your shell environment into the sandbox. Use `--env NAME` to pass a variable from the parent environment, or `--env NAME=VALUE` to define one on the fly:

```bash
bulle --rox /bin --env USER -- /bin/echo $USER
bulle --rox /usr/bin/printenv --env HELLO=WORLD -- /usr/bin/printenv HELLO
```

This is important for secrets. A command cannot read `OPENAI_API_KEY`, `GITHUB_TOKEN`, or similar variables unless you explicitly pass them.

## Profiles

Coding agents often need shells, package managers, language runtimes, caches, config files, and app storage. Profiles collect those repeated path and environment grants in one named bundle.

Use `--policy` to inspect a profile's grants before launching. You can still add one-off permissions to a profile with the same path and environment flags:

```sh
bulle --profile claude --ro README.qmd --rw ~/Desktop --env GITHUB_TOKEN --policy
```

Profiles live in the global `bulle` config file. By default, `bulle` reads `config.toml` from the operating system's user config directory, under a `bulle` subdirectory: on Linux and other XDG systems this is usually `$XDG_CONFIG_HOME/bulle/config.toml` or `~/.config/bulle/config.toml`; on macOS it is usually `~/Library/Application Support/bulle/config.toml`. Use `--config PATH` to load a different global config file. `bulle` does not read project-local config files.

A profile can define a default app, filesystem grants, environment variables, and inheritance:

```toml
default_profile = "tool"

[profiles.tool]
rw = ["$TMP/bulle/tmp"]
env = ["PATH"]
add_exec = true
add_libs = true

[profiles.agent]
inherits = "tool"
default_app = "codex"
rw = ["$HOME/.cache/example-agent"]
env = ["HOME", "USER", "TERM", "LANG", "SHELL", "OPENAI_API_KEY"]
```

List fields are inherited and appended by default. Set `replace_ro`, `replace_rox`, `replace_rw`, `replace_rwx`, or `replace_env` in a child profile to replace the corresponding inherited list instead.

## Policy

Use `--policy` to inspect the resolved sandbox policy without running the command. This is a useful safety check before launching an agent or script, especially when combining profiles with extra filesystem or environment grants.

```bash
bulle --policy ~/Desktop --rox /bin -- /bin/ls
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
  "allow_keychain": false
}
```

In this example, `workspace_path` is the directory where the command would run. Because workspaces are granted automatically by default, the command would run with read-write access to `/home/user/Desktop`, shown in the `rw` array. The `command` field is the command that would be executed, and the `ro`, `rox`, `rw`, and `rwx` arrays show the readable, executable, writable, and writable-executable path grants. The `env_keys` array lists environment variables that would be passed into the sandbox. The `backend` value depends on your operating system.

## Executables and libraries

For quick local commands, `--add-exec` can save you from spelling out executable grants by hand. It resolves the command before the sandbox starts and adds the executable to the policy:

```bash
bulle --add-exec -- /bin/ls
```

On Linux, dynamically linked executables also need access to runtime libraries. `--add-libs` discovers the shared libraries needed by the executable and adds read-only grants for them:

```bash
bulle --add-exec --add-libs -- /usr/bin/git status
```

These flags are conveniences for executables and runtime libraries. They do not add app state files, config directories, caches, secrets, or shell environment variables. Use profiles for agents and other tools that need a larger, repeatable policy.

Profiles can enable these conveniences with `add_exec = true` and `add_libs = true`. Boolean capabilities are inherited with OR semantics: once a parent enables one, a child profile cannot disable it. Use a different parent profile when you need a stricter variant.

## How it works

`bulle` builds a policy before the command starts. The policy is assembled from the workspace, selected profile, command-line flags, selected environment variables, executable discovery, and runtime library defaults. Paths are resolved before sandbox setup, and `--policy` prints the resulting policy without running the command.

### Linux

On Linux, `bulle` applies the policy with [Landlock](https://docs.kernel.org/userspace-api/landlock.html). Landlock is a kernel feature, not a package to install; basic filesystem sandboxing requires Linux 5.13 or later with Landlock enabled. The Linux backend restricts filesystem access for the process and its children according to the resolved read, write, and execute grants.

### macOS

On macOS, `bulle` generates a [Seatbelt](https://www.unix.com/man_page/osx/5/sandbox/) profile and runs the command with `/usr/bin/sandbox-exec`. The macOS backend maps the same policy model to Seatbelt rules, including filesystem rules, network allowance, and selected Mach service access such as Keychain support when a profile enables it. This is useful for local workflows, but its behavior is not identical to Linux Landlock.

## License and attribution

`bulle` is distributed under the MIT License. See [LICENSES/bulle-MIT.txt](LICENSES/bulle-MIT.txt).

Thank you to [Landrun](https://github.com/Zouuup/landrun), an excellent, compact Go implementation of practical Landlock sandboxing. The Linux sandbox backend and filesystem permission model owe a clear debt to Landrun's design, and portions of the Linux backend and ELF dependency discovery are derived from or inspired by Landrun. See [LICENSES/landrun-MIT.txt](LICENSES/landrun-MIT.txt) for the full third-party notice and license.
