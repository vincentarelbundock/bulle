#set document(title: [CLI Reference])
#metadata((
  title: "CLI Reference",
  description: "Command-line reference for bulle.",
)) <website-metadata>

#title()

This page is generated from bulle --help.

````text
bulle runs local coding agents inside a controlled workspace.

It exposes one CLI and one policy model across Linux and macOS. Linux uses an
embedded Landlock backend; macOS uses a generated Seatbelt profile. The sandbox
restricts filesystem and environment access, and can optionally deny network access.

Usage:
  bulle [flags] [workspace] [-- command [args...]]
  bulle policy [run flags] [--json]
  bulle rerun [extra flags]
  bulle scratch [run flags] [workspace] [-- command [args...]]
  bulle scratch list|diff|pull|wipe|shell [id]
  bulle profiles list|install SOURCE
  bulle resolvers
  bulle record --profile NAME [--out FILE] [--max-rounds N] -- command [args...]
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
  scratch           run in a disposable clone, or manage kept ones (see below)
  profiles list     list available profiles
  profiles install SOURCE
                    install profile TOML files from a file, directory, local
                    git repository, or GitHub source
  resolvers         list path resolvers and what they resolve to here
  record            run a command repeatedly, widening the sandbox by what it
                    was denied each time, and print a profile of the additions
                    (see below)

Configuration:
  --config PATH     path to a configuration directory
  --var NAME=VALUE  define a custom path variable usable in grants as $NAME
  --no-defaults     ignore the [defaults] block of the user configuration

Profiles:
  -p, --profile NAME
                    named profile, or comma-separated profiles merged left to right
  claude            Claude Code app state, config, and login support
  codex             Codex app state, config, network, MCP, and login support
  default           default sandbox profile
  git               git binary, user configuration, and global ignores
  go                Go toolchain, module cache, and build cache
  keychain          macOS Keychain file and service access
  latex             TeX Live engines, the distribution tree, and font caches
  macos-certs       macOS certificate trust service lookup
  macos-devtools    the active Xcode or Command Line Tools developer directory
  macos-dns         macOS DNS and directory service lookups
  network           network socket access capability
  node              Node.js runtime, npm, and package caches
  offline           deny network socket access
  opencode          OpenCode app state and config support
  pandoc            pandoc document converter and its data directory
  pi                Pi app state and config support
  quarto            quarto rendering with pandoc, LaTeX, R, and Python support
  r                 R interpreter, library search path, and startup files
  r-install         R plus a writable user library and CRAN access
  rust              cargo, rustup toolchains, and the registry cache
  terminal          environment variables an interactive terminal program expects
  tool              general local command support (PATH, executables, libs)
  user-bin          personal executable directories (~/.local/bin, ~/.bin, ~/.cargo/bin)
  uv                uv-managed Python interpreters and environments
  uv-install        uv plus a writable cache, project venv, and PyPI access

Executable discovery:
  --add-exec        add the resolved command executable to the sandbox
  --add-libs        add runtime library access for executables
                    Linux discovers ELF shared-library dependencies precisely.
                    macOS uses configured runtime roots; Mach-O dylibs in
                    unusual locations may need explicit --ro/--rox grants.
                    These do not add app state files or environment variables.

Disposable workspaces:
  bulle scratch [run flags] [workspace] [-- command [args...]]
                    run against a disposable local clone of the workspace
                    (git repositories only; uncommitted work is carried over).
                    After the run, review the changes: diff, pull into the
                    origin, keep, open a shell in the scratch, or wipe it.
                    Everything after "scratch" that is not a review verb is an
                    ordinary run; use -- to run a command named like a verb.
  --scratch         the same, as a flag on a run — for composing with the other
                    subcommands, as in bulle rerun --scratch
  --scratch-keep    skip the review prompt, keep the scratch, print its path
  bulle scratch list|diff|pull|wipe|shell [id]
                    resume the review of a kept scratch later; id may be a
                    unique prefix, and is optional when unambiguous

Profile recording:
  bulle record --profile NAME -- command [args...]
                    run the command under NAME, collect what the sandbox
                    denied, add those grants, and run again until nothing new
                    is denied. Prints a profile inheriting from NAME that
                    contains only the additions.
  --out FILE        write the profile to FILE instead of stdout
  --max-rounds N    stop after N rounds (default 10); reaching the cap with the
                    command still failing is reported, and the profile says so

                    A recording is evidence that one run of one command needed
                    these grants — not that they are sufficient. A denial
                    aborts the operation that hit it, so anything the command
                    did not reach is missing. Review before installing.

                    On macOS the unified log reports violations from every
                    sandboxed process, and a violation names a pid that has
                    already exited, so entries may not all be yours. Each one
                    names the process it was denied to.

Output and safety:
  --timeout DURATION
                    kill the sandboxed command if it runs longer than DURATION.
                    Uses Go duration syntax such as 30s, 2m, or 1h30m.
                    Use 0 to disable. Timed-out commands exit 124.

Resource limits:
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
                    --memory, --cpu, and --nproc need cgroup v2, so they apply
                    on Linux with a delegated cgroup and nowhere else; macOS
                    has no per-process-tree equivalent for any of the three.
                    The others are portable. bulle warns on stderr about any
                    limit it cannot enforce, and bulle policy names the
                    mechanism behind each one. Set a limit under
                    [defaults.linux] to request it only where it works.

  -h, --help        show this help and exit
  -V, --version     show version information and exit

Examples:
  bulle --profile claude ~/repos/project   # Claude Code in a workspace
  bulle scratch --profile claude           # the same, in a disposable clone
  bulle --add-exec -- /bin/ls
  bulle --profile codex --ro ~/.cache/uv
  bulle . --profile secrets --env OPENAI_API_KEY -- codex
  bulle policy --json . --rox /bin -- /bin/bash
  bulle policy --profile codex
  bulle --profile codex,offline
  bulle profiles install agent.toml
  bulle profiles install github:vincentarelbundock/bulle/custom_profiles
````
