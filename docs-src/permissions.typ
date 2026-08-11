#import "/.calepin/calepin.typ" as calepin

#set document(title: [Permissions])
#metadata((
  title: "Permissions",
  description: "Filesystem and environment grants, network access, policy inspection, configuration defaults, and how the sandbox is enforced.",
)) <website-metadata>

#title()

Every run resolves to one policy: the paths the command may read, write, or execute, the environment variables it inherits, and whether it may reach the network. This page covers the grants you write by hand, how to inspect the result, and how the operating system enforces it. #link("profiles.html")[Profiles] bundle the same grants under a name so you do not repeat them.

= Filesystem
<filesystem>

The workspace is the command's working directory and writable area. If omitted, it defaults to the current directory. Use `--no-workspace` when you do not want this automatic read-write grant.

Additional filesystem access is explicit. Use these flags to add paths to the active policy:

```bash
--ro path        # read-only
--rox path       # read-only plus execute
--rw path        # read-write
--rwx path       # read-write plus execute
--no-workspace   # do not automatically grant the workspace read-write access
```

#calepin.elements.callout(kind: "note", title: none)[
  Grant the narrowest paths that are practical. Use `--rw` or `--rwx` only for paths outside the workspace that the command should be allowed to modify.
]

The `--` separator may be omitted when the reading is unambiguous. The first positional argument that is an existing directory reads as the workspace; the first positional that is not an existing directory starts the command, and `bulle` announces the split on stderr:

```bash
bulle ~/repos/project git status
# bulle: treating "git" as the start of the command; use -- to separate the command explicitly
```

Ambiguity resolves toward the workspace reading, so `--` remains the explicit way to force a directory name to be treated as a command.

Path entries written in a profile can also use variables, globs, and optional/create markers so one entry works on every machine; see #link("profiles.html#portable-profiles")[Portable profiles].

= Environment
<environment>

Environment variables are also explicit. By default, `bulle` does not pass your shell environment into the sandbox. Use `--env NAME` to pass a variable from the parent environment, or `--env NAME=VALUE` to define one on the fly:

```bash
bulle --rox /usr/bin --env HELLO=WORLD -- printenv HELLO
```

This is important for secrets. A command cannot read `OPENAI_API_KEY`, `GITHUB_TOKEN`, or similar variables unless you explicitly pass them.

Three conveniences cover common cases:

- *Name globs.* `--env 'GIT_*'` passes every parent variable whose name matches the glob. Quote the pattern so your shell does not expand it. Globs also work in profile `env` lists.
- *Dotenv files.* `--env-file PATH` loads `NAME=VALUE` entries from a dotenv-style file: blank lines and `#` comments are ignored, an optional `export ` prefix is stripped, and matching single or double quotes around values are removed.
- *Everything except.* `--env-all-except SECRET_KEY,GITHUB_TOKEN` passes the whole parent environment minus the named variables, for throwaway commands that want your shell environment without specific secrets.

When sources conflict, the most explicit wins: profile entries, then `--env-all-except`, then `--env-file`, then `--env`.

The summary and JSON views list environment variables by name only; neither view prints their values.

= Network
<network>

Network access is controlled by profiles. The built-in `network` profile allows it, and the built-in `offline` profile denies it. On macOS, the `network` profile also inherits DNS and certificate service bundles that network clients normally need. Built-in agent profiles inherit network access for compatibility with package managers and remote services.

```bash
bulle --profile offline --rox /bin -- /bin/ls
bulle --profile codex,offline
```

In a profile the capability is named `network`, so `allow = ["network"]` enables access and `deny = ["network"]` disables it.

= Executables and libraries
<executables-and-libraries>

For quick local commands, `--add-exec` can save you from spelling out executable grants by hand. It resolves the command before the sandbox starts and adds the executable to the policy:

```bash
bulle --add-exec -- /bin/ls
```

On Linux, dynamically linked executables also need access to runtime libraries. `--add-libs` discovers the shared libraries needed by the executable and adds read-only grants for them:

```bash
bulle --add-exec --add-libs -- /usr/bin/git status
```

These flags are conveniences for executables and runtime libraries. They do not add app state files, config directories, caches, secrets, or shell environment variables. Use #link("profiles.html")[profiles] for agents and other tools that need a larger, repeatable policy.

Profiles can enable these conveniences with `add_exec = true` and `add_libs = true`. Boolean settings inherit like other scalar profile settings: an explicit value in a later inherited profile or child profile overrides the earlier value.

= Inspecting the policy
<policy>

Use `bulle policy` to inspect the resolved sandbox policy without running the command. By default, it prints the same human-readable permissions summary that `bulle` sends to supported LLM agent profiles at startup. This is a useful safety check before launching an agent or script, especially when combining profiles with extra filesystem or environment grants.

```bash
bulle policy --profile codex
```

No command is required: without one (and without a configured `default_app`), the policy is resolved and printed as-is, minus command-dependent grants such as `--add-exec` and shebang interpreter discovery.

== The resolution table

The summary ends with a *resolution table*: one line per configured entry showing what it resolved to --- granted, skipped, created, or expanded from a `which:`/`pkg:` resolver or glob --- so "why can't the agent see X" is answerable from one command. Entries whose resolved path is also granted through another list (for example when `$CONFIG` and `$DATA` collapse to the same directory on macOS) are flagged with their effective permission.

```text
  resolution:
    rox  which:codex        → /home/user/.local/share/mise/installs/node/22/bin/codex (+1 more)
    rwx  +$HOME/.codex/     → created (dir) /home/user/.codex
    rw   ?$HOME/.netrc      → skipped (does not exist)
```

== JSON output

Stable machine-readable output is available with `bulle policy --json`:

```bash
bulle policy --json ~/Desktop --rox /bin -- /bin/ls
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

In the `bulle policy --json` example, `workspace_path` is the directory where the command would run. Because workspaces are granted automatically by default, the command would run with read-write access to `/home/user/Desktop`, shown in the `rw` array. The `command` field is the command that would be executed, and the `ro`, `rox`, `rw`, and `rwx` arrays show the readable, executable, writable, and writable-executable path grants. The `env_keys` array lists environment variables that would be passed into the sandbox. The `mach_lookup` array lists configured macOS Mach services. The `network` field shows the resolved network state. The `backend` value depends on your operating system.

= Configuration defaults
<configuration-defaults>

A `[defaults]` block in `<config>/config.toml` (usually `~/.config/bulle/config.toml`) supplies values used when the corresponding flag is absent, so bare `bulle` does the usual thing in a repository:

```toml
[defaults]
profile = "claude"
timeout = "2h"
env = ["GITHUB_TOKEN"]
ro = ["?~/.gitconfig"]
```

Explicit flags always win: `--profile codex` overrides the default profile, `--timeout` the default timeout, and list-valued defaults (`env`, `ro`, `rox`, `rw`, `rwx`) are merged with command-line entries taking precedence. Pass `--no-defaults` to ignore the block entirely.

The same file holds the `[vars]` table used by #link("profiles.html#portable-profiles")[portable profiles], and the `[scratch]` table that relocates #link("scratch.html")[scratch workspaces].

= OS-level sandboxing
<os-level-sandboxing>

`bulle` builds a policy before the command starts. The policy is assembled from the workspace, selected profile, command-line flags, selected environment variables, network profile settings, executable discovery, and runtime library defaults. Paths are resolved before sandbox setup, and `bulle policy` prints the resulting policy without running the command.

== Linux

On Linux, `bulle` applies the policy with #link("https://docs.kernel.org/userspace-api/landlock.html")[Landlock]. Landlock is a kernel feature, not a package to install; basic filesystem sandboxing requires Linux 5.13 or later with Landlock enabled. The Linux backend restricts filesystem access for the process and its children according to the resolved read, write, and execute grants. When the resolved network setting is denied, it also installs a seccomp filter before `exec` to deny socket-related system calls.

== macOS

On macOS, `bulle` generates a #link("https://www.unix.com/man_page/osx/5/sandbox/")[Seatbelt] profile and runs the command with `/usr/bin/sandbox-exec`. The macOS backend maps the same policy model to Seatbelt rules, including filesystem rules, optional network allowance, and selected Mach service access from configured `mach_lookup` entries. This is useful for local workflows, but its behavior is not identical to Linux Landlock.

When a run fails because the kernel blocked something, #link("denial-diagnostics.html")[denial diagnostics] report which path it was.
