# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`--scratch` disposable workspaces.** Run the sandboxed command against a
  throwaway local clone of the workspace instead of the real checkout. The
  clone carries uncommitted work (tracked changes and untracked files) so the
  agent starts from the state you see; git objects are hardlinked, so cloning
  is nearly free. The real checkout is never granted to the sandbox. After
  the run, a review gate shows what changed — including work the agent
  committed — and offers diff, keep, a subshell inside the scratch, or
  discard; a run that changed nothing
  cleans up after itself, and a scratch with changes is never deleted
  implicitly. Integration is git-native: keep prints a `git push
  origin HEAD:scratch/<id>` recipe, and the result lands as a ref in the
  origin, never as working-tree changes. `--scratch-keep` skips the prompt
  for non-interactive runs, `[scratch] dir` in `config.toml` relocates
  scratches (with a warning when the location defeats hardlinking), and
  denial hints from scratch runs are rewritten to origin-relative paths so
  suggested grants stay meaningful. Worktree-based isolation is deliberately
  not offered: worktrees share the origin's `.git`, including hooks.
- **`--policy` works without a command.** `bulle -p uv --policy` prints the
  resolved policy even when no command is supplied and no `default_app` is
  configured; command-dependent grants (`add_exec`, shebang discovery) are
  simply absent from the output.
- **Built-in `r`, `r-install`, `uv`, and `uv-install` profiles.**
  `bulle --profile r -- Rscript analysis.R` and
  `bulle --profile uv -- uv run script.py` work on conventional installs
  (verified on Nix, the adversarial layout) without hand-written grants. Base
  profiles are offline with read-only libraries; the `-install` variants add a
  writable user library or cache and network access. The `uv` base profile
  sets `UV_NO_CACHE=1` so offline runs use an ephemeral cache inside the
  sandbox tmp instead of needing write access to the real one.
- **Exec-chain-aware library discovery.** With `add_libs`, bulle now scans the
  granted trees for ELF objects (exec trees fully, read-only trees under
  `libs/` directories), follows wrapper scripts' shebang interpreters and
  package-store references, and grants the combined `DT_NEEDED`/`RPATH`
  closure with package stores trusted as RPATH roots. This makes interpreters
  reached through several exec hops (Nix wrappers, version managers) work
  without manual grants. The scan is budgeted, and a truncated scan is
  reported as a policy note. It also follows symlinks between store items
  (gcc's `libgcc_s` living in a separate output), `nix-support` flag files
  (propagated link-time inputs), and store paths baked into the dynamic
  loader itself (Nix patches ld.so with a default libgcc directory used for
  lazy unwinder loads that appear in no ELF header).
- **Denial hints collapse package-store paths.** Denials inside one Nix store
  item or Homebrew keg now produce a single suggested grant for the package
  root, so the `bulle --last` retry line converges in one step instead of one
  file at a time.
- **Profile smoke-test harness.** A table-driven verification suite
  (`internal/integration/profile_smoke_test.go`) runs each shipped profile
  against its tool; CI runs it on Ubuntu, macOS, and Nix. A profile authoring
  guide with the slot checklist and house rules ships in the website docs.

### Changed

- **`deny = ["network"]` no longer blocks `socketpair(2)` on Linux.** A
  socketpair is a connected in-process pipe (Linux only supports `AF_UNIX`
  pairs) and cannot reach outside the sandbox, while async runtimes (tokio,
  libuv) need one for signal handling before doing any real work. `socket`,
  `connect`, `bind`, `listen`, and `accept` remain denied.
- **The `tool` profile grants common runtime probes.** `/bin/sh` and `bash`
  (rox), `/dev/null` and `/dev/tty` (rw), and read-only `/proc/stat`,
  `/proc/cpuinfo`, cgroup limits, CPU topology, transparent-hugepage flags,
  timezone data, `/etc/os-release`, glibc locale archives, and the NixOS
  nix-ld loader directory — files that shells, OpenMP, BLAS, allocators, and
  language runtimes touch on startup. `NIX_LD`, `NIX_LD_LIBRARY_PATH`, and
  `LD_LIBRARY_PATH` pass through so foreign binaries (rustup toolchains)
  resolve their libraries the same way they do outside the sandbox.
- **The `git`, `go`, `node`, and `rust` profiles work standalone and are
  smoke-tested.** They now inherit `tool` and `terminal` instead of being
  mixin-only. `git` grants its package tree for libexec subcommands and its
  config files by name (so symlinked dotfiles carry their targets); `go` can
  `go run` (executable sandbox tmp, telemetry dir); `rust` can
  `cargo run --offline` end to end (linker via `cc`/`ld`, executable
  `target/`, the cargo global cache, git config for vcs metadata).

- **Tool resolvers: `TOOL:ASPECT` path entries.** A grant can ask a tool where
  its own directories are instead of hard-coding them: `r:home`, `r:prefix`,
  `r:libs`, `r:libs-user`, `uv:cache`, `uv:tools`, `uv:python`, `go:root`,
  `go:path`, `go:modcache`, and `npm:cache`. Unlike `which:`/`pkg:` these name
  ordinary directories and are valid in every list, support the `?` and `+`
  markers, and are re-resolved as literal paths so the symlink and
  sensitive-target checks apply. The set is a fixed registry in `bulle`; a
  profile can never supply a command to run. Unknown namespaces are an error
  rather than a literal path, and `r:prefix` drops results that would be a
  system root. Motivated by R's package search path, which holds one entry per
  installed package (70 on a Nix machine) and cannot be written down.
- **`--list-resolvers`.** Prints every resolver with what it resolves to on the
  current machine, so a resolver returning nothing is distinguishable from one
  that works.
- **`terminal` capability profile.** The environment variables an interactive
  terminal program expects, previously repeated in every agent profile.

### Changed

- **Agent profiles assemble from capability profiles.** `claude`, `codex`,
  `opencode`, and `pi` now inherit `terminal`, `git`, and `node` rather than
  restating environment lists, so they carry git configuration and the Node
  toolchain that agents routinely shell out to. Codex's state directory follows
  `${CODEX_HOME:-$HOME/.codex}`. The `node` and `go` profiles ask `npm` and
  `go env` for their cache locations, with the previous literal paths as
  fallbacks.

### Fixed

- **Markers survive resolver expansion.** `?` and `+` on a resolver entry were
  dropped when it expanded, so an optional resolver whose directory did not yet
  exist became a hard failure in a `rw` list.

- **Rerun with an added grant: `--last`.** Every real run records its
  invocation (arguments and working directory) under the user state
  directory. `bulle --last` repeats it from any shell, inserting extra flags
  before the command, and denial diagnostics now end with a copy-pasteable
  `bulle: retry with these grants: bulle --last --ro ...` line. The sandbox
  restarts rather than widens.
- **Configuration defaults: `[defaults]` block.** `config.toml` can supply
  `profile`, `timeout`, `env`, and path grants used when the corresponding
  flag is absent, so bare `bulle` does the usual thing in a repository.
  Explicit flags win; `--no-defaults` ignores the block.
- **The `--` separator may be omitted when unambiguous.** The first
  positional that is an existing directory reads as the workspace; the first
  positional that is not starts the command, announced on stderr. Ambiguity
  resolves toward the workspace, and `--` remains the explicit override.
- **Capability micro-profiles: `git`, `node`, `rust`, `go`.** Small built-in
  profiles that answer one tool's location questions portably (binaries via
  `which:`/`pkg:`, configuration, caches), meant to be assembled through
  `inherits` or combined with `--profile tool,git,rust`.
- **Environment conveniences.** `--env 'GIT_*'` name globs against the
  parent environment (also in profile `env` lists), `--env-file PATH` for
  dotenv-style files, and `--env-all-except NAME,...` for the whole parent
  environment minus named secrets.

- **Portable profiles: path entries now adapt to the machine.** A profile
  written on one machine works on another:
    - `?path` marks a writable entry optional (skip when missing); `+path/`
      creates a missing directory and `+path` a missing file. Read-only
      entries keep their existing skip-if-missing behavior. The built-in
      `claude`, `codex`, `opencode`, and `pi` profiles now create their state
      directories on first run instead of failing on a fresh machine.
    - `which:NAME` resolves a command on the parent `PATH` and grants exactly
      that binary (plus its symlink chain) — never its containing directory.
      Name lookup inside the sandbox goes through a per-run, read-only shim
      directory that `bulle` creates and removes automatically. `pkg:NAME`
      additionally grants the tool's package tree, refusing system
      directories such as `/usr`.
    - Platform-neutral variables `$CONFIG`, `$DATA`, `$CACHE`, and `$STATE`
      resolve to XDG directories on Linux (honoring `XDG_*` overrides) and
      `~/Library` equivalents on macOS.
    - Single-star globs (`~/.nvm/versions/node/*/bin`); no matches means the
      entry is skipped.
    - Custom variables from a `[vars]` table in `<config>/config.toml` or
      `--var NAME=VALUE`, with `${NAME:-fallback}` defaults. A small
      allowlist of well-known tool environment variables (`CARGO_HOME`,
      `GOPATH`, `NVM_DIR`, ...) may be referenced; untrusted values that are
      relative or resolve to `/` or the home directory are ignored.
- **Resolution table in `--policy`.** The policy summary now ends with one
  line per configured entry showing its outcome (granted, skipped, created,
  resolver expansion), and flags entries whose grants collapse into a
  stronger permission on the current platform.

- **Denial diagnostics: failed runs explain what the sandbox blocked.** After a
  sandboxed command fails, `bulle` reads the operating system's own record of
  sandbox denials and prints copy-pasteable fixes, e.g.
  `denied: read /home/user/.gitconfig — add --ro ~/.gitconfig`. On Linux this
  uses Landlock audit records (kernel 6.15+ with the audit subsystem enabled);
  on macOS it queries the unified log, which needs no setup. Hints are
  best-effort and print nothing when denial records are unavailable. See the
  new *Denial Diagnostics* documentation page, including one-line setup
  instructions per Linux distribution.
- **Profile inference: `bulle -- claude` selects the matching profile.** When
  no `--profile` is given and the command cannot run under the default
  profile, `bulle` checks whether exactly one installed profile declares that
  command as its `default_app`, selects it, and announces the choice on
  stderr. Inference only rescues runs that would otherwise fail command
  discovery, never overrides an explicit `--profile`, and refuses to guess
  when several profiles match. Works out of the box with the built-in
  `claude`, `codex`, `opencode`, and `pi` profiles.

### Changed

- **All runs are now supervised.** Runs without `--timeout` previously
  replaced the `bulle` process with the sandboxed command directly; every run
  now keeps a lightweight parent process, which unifies signal forwarding and
  terminal handling across timed and untimed runs and makes post-run denial
  diagnostics possible. Exit codes and signal behavior are unchanged.
- Upgraded `go-landlock` to v0.9.0 to enable Landlock audit logging of
  sandbox denials on supporting kernels. Enforcement semantics are unchanged.

## [0.0.7] - 2026-08-10

This release hardens sandbox enforcement and policy resolution following a
security review. One change to the macOS backend is behavioral and may require
existing profiles to broaden their executable roots — see **Changed** below.

### Changed

- **macOS: process execution is now restricted to granted executable roots.**
  The Seatbelt profile previously allowed `process-exec*` unconditionally, so a
  confined process could launch any binary on the system. Execution is now
  scoped to the `rox`/`rwx` roots the policy actually grants, keeping it
  consistent with the existing `file-map-executable` rules and removing a
  springboard out of the sandbox. The command you run is always covered
  automatically; if a tool your agent shells out to is now blocked, add its
  directory with `--rox`/`--rwx`, use `--add-exec`, or select a profile that
  grants it.

### Security

- **Config: top-level `deny` is enforced as a guardrail.** A capability denied
  at the top level (`deny` / `deny_mach_lookup`) can no longer be silently
  re-granted by an explicitly selected profile. Top-level *allows* remain
  defaults that a selected profile may override, but denies win in both
  directions.
- **Paths: reject symlinked grants that resolve to sensitive locations.** A
  configured path whose symlink target resolves to the filesystem root or the
  home directory is now refused, closing a cross-run escalation where a grant
  could be repointed at a sensitive directory. Home comparison uses filesystem
  identity, so it holds even when `$HOME` is itself a symlink.
- **Linux: no descriptor leaks when `/proc` is unavailable.** The
  file-descriptor cleanup fallback now uses `close_range(2)` and, where that is
  unavailable, honors `RLIMIT_NOFILE` instead of a fixed 1024 ceiling — an
  unlimited limit no longer wrapped back to 1024 and left high descriptors open.

### Fixed

- **Supervisor: avoid signaling a recycled process group.** After the process
  leader is reaped on timeout, the supervisor now probes the group before
  sending `SIGKILL`, so it does not signal an unrelated same-user process group
  whose PGID was recycled. (A residual probe-to-signal race remains; fully
  closing it requires cgroup- or PID-namespace-based supervision, planned
  separately.)

[Unreleased]: https://github.com/vincentarelbundock/bulle/compare/v0.0.7...HEAD
[0.0.7]: https://github.com/vincentarelbundock/bulle/compare/v0.0.6...v0.0.7
