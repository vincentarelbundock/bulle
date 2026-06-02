---
title: CLI Reference
description: Command-line reference for bulle.
hide:
  - navigation
---

# CLI Reference

This page is generated from bulle --help.

~~~text
bulle runs local coding agents inside a controlled workspace.

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

Configuration:
  --config PATH     path to a configuration directory

Profiles:
  -p, --profile NAME
                    named profile, or comma-separated profiles merged left to right
  --list-profiles  list available profiles and exit
  --install-profiles SOURCE
                    install profile TOML files from a file, directory, local git repository, or GitHub source
  claude            Claude Code app state, config, and login support
  codex             Codex app state, config, network, MCP, and login support
  default           default sandbox profile
  keychain          macOS Keychain file and service access
  macos-certs       macOS certificate trust service lookup
  macos-dns         macOS DNS and directory service lookups
  network           network socket access capability
  offline           deny network socket access
  opencode          OpenCode app state and config support
  pi                Pi app state and config support
  tool              general local command support (PATH, executables, libs)

Executable discovery:
  --add-exec        add the resolved command executable to the sandbox
  --add-libs        add runtime library access for executables
                    Linux discovers ELF shared-library dependencies precisely.
                    macOS uses configured runtime roots; Mach-O dylibs in
                    unusual locations may need explicit --ro/--rox grants.
                    These do not add app state files or environment variables.

Output and safety:
  --timeout DURATION
                    kill the sandboxed command if it runs longer than DURATION.
                    Uses Go duration syntax such as 30s, 2m, or 1h30m.
                    Use 0 to disable. Timed-out commands exit 124.
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
  bulle --profile codex,offline
  bulle --install-profiles agent.toml
  bulle --install-profiles ./profiles
  bulle --install-profiles github:vincentarelbundock/bulle/custom_profiles
~~~
