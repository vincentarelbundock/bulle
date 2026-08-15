# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### Profile recording

- **`bulle record` drafts a profile from a real run.** `bulle record --profile
  <base> -- <command>` runs the command under an existing profile, collects
  what the sandbox denied, adds those grants, and runs again until nothing new
  is denied. It prints a profile inheriting from the base that contains only
  the additions, so recording against `claude` does not restate everything
  `tool`, `terminal`, `git`, and `node` already grant. Linux only, for now:
  macOS logs Seatbelt violations too, but denies many benign probes the
  Landlock path never sees, and emitting those as grants would produce a
  profile far wider than the run needs.
- **Recorded entries are written to travel.** Paths are rewritten into the
  spelling a hand-written profile would use — `go:modcache` rather than one
  machine's module cache, `$CONFIG/...` rather than `$HOME/.config/...`,
  `which:NAME` for a binary that is simply what `PATH` resolves — and every
  entry is optional, because a path this machine has is not one the next
  machine has. Clusters of denied siblings collapse into a directory grant. A
  denial on a variable root never becomes a grant on that root.
- **The output says what it proves.** A denial aborts the operation that hit
  it, so a recording is evidence that one run of one command needed these
  grants, not that they are sufficient; the emitted header says so, and says
  more when the command still failed or the round cap was reached. Recording
  refuses to start unless a deliberately triggered denial actually reaches the
  kernel log — a kernel can advertise Landlock audit support with auditing
  disabled, and recording would otherwise converge instantly on an empty
  profile that reads exactly like success.

#### Resource limits

- **Resource limits on a run.** `--memory`, `--cpu`, `--nproc`, `--nofile`,
  `--fsize`, and `--cpu-time` cap what a sandboxed command consumes, alongside
  the existing wall-clock `--timeout`. They are configurable under
  `[defaults.limits]`, and under `[defaults.linux.limits]` or
  `[defaults.macos.limits]` to request a limit only where it applies.
- **Honest reporting of what is enforced.** `--memory`, `--cpu`, and `--nproc`
  need cgroup v2, so they bind on Linux with a delegated cgroup and nowhere
  else. macOS has no per-process-tree equivalent, and the POSIX limits that
  resemble one are not substitutes: `RLIMIT_AS` caps virtual address space
  rather than resident memory, killing runtimes that merely reserve it, and
  `RLIMIT_NPROC` counts processes per UID across the whole system, which would
  throttle the user's unrelated programs. bulle declines to substitute them,
  warns on stderr about any limit it cannot enforce, and names the mechanism
  behind each limit in `bulle policy` output. `--strict-limits` turns the
  warning into a refusal to run, for continuous integration.
- **Timeouts now kill through the cgroup when there is one.** A cgroup-backed
  run terminates via `cgroup.kill`, which identifies the process tree directly.
  This closes the PGID-recycling race that group-signalling could not, and
  catches processes that called `setsid` to leave the group. Runs without a
  cgroup keep the previous best-effort group-signalling behavior.

#### Workspaces

- **`bulle scratch` disposable workspaces.** Run the sandboxed command against
  a throwaway local clone of the workspace instead of the real checkout. The
  clone carries uncommitted work (tracked changes and untracked files) so the
  agent starts from the state you see; git objects are hardlinked, so cloning
  is nearly free, and the real checkout is never granted to the sandbox.
  After the run, a review gate shows what changed — including work the agent
  committed — and offers diff, pull-into-origin, keep, a subshell inside the
  scratch, or wipe. Pull never commits on your behalf (a dirty scratch routes
  through the subshell to commit first), and a failed pull changes nothing
  and keeps the scratch. A run that changed nothing cleans up after itself; a
  scratch with changes is never deleted implicitly. Integration is
  git-native: keep prints a one-command `git pull` recipe run from the origin
  side (with a warning when the scratch has uncommitted changes, since pull
  only moves commits) and a fetch-to-ref variant for inspecting before
  merging. A kept scratch is a paused review:
  `bulle scratch list|diff|pull|wipe|shell [id]` resumes it later with the
  same semantics as the prompt letters (id optional when unambiguous, unique
  prefixes accepted). The subcommand covers creation too: anything after
  `scratch` that is not a review verb is an ordinary run
  (`bulle scratch --profile claude`), with `--` available for a command named
  like a verb and a typo guard so a mistyped verb reports itself instead of
  cloning. The flag form `--scratch` remains, and is what `bulle rerun` and
  `bulle policy` compose with. `--scratch-keep` skips the prompt for non-interactive
  runs, `[scratch] dir` in `config.toml` relocates scratches (warning when
  the location defeats hardlinking), and denial hints from scratch runs are
  rewritten to origin-relative paths so suggested grants stay meaningful.
  Worktree-based isolation is deliberately not offered: worktrees share the
  origin's `.git`, including hooks.

#### Language and tool profiles

- **`user-bin` profile; personal bin directories are no longer default.**
  `~/.local/bin`, `~/.bin`, and `~/.cargo/bin` moved out of the platform
  default executable roots into an opt-in `user-bin` profile: execute
  requires read under both backends, and personal scripts in those
  directories can hold secrets a default sandbox should not see. Agent
  profiles (`claude`, `codex`, `opencode`, `pi`) inherit `user-bin`; other
  runs add `--profile user-bin` when a user-installed helper is denied.
- **Built-in `r`, `r-install`, `uv`, and `uv-install` profiles.**
  `bulle --profile r -- Rscript analysis.R` and
  `bulle --profile uv -- uv run script.py` work on conventional installs
  (verified on Nix, the adversarial layout) without hand-written grants. Base
  profiles are offline with read-only libraries; the `-install` variants add a
  writable user library or cache and network access. The `uv` base profile
  sets `UV_NO_CACHE=1` so offline runs use an ephemeral cache inside the
  sandbox tmp instead of needing write access to the real one.
- **Built-in `latex`, `pandoc`, and `quarto` profiles.** `latex` asks
  `kpsewhich` where the TeX trees live (new `tex:dist`, `tex:sysvar`,
  `tex:var`, `tex:config`, and `tex:home` resolvers) and keeps the per-user
  runtime trees writable for format and font-cache regeneration; verified
  with pdflatex and lualatex. `pandoc` is a thin converter profile. `quarto`
  is the composition showcase: it inherits `pandoc`, `latex`, `r`, and `uv`,
  grants the directories `quarto --paths` reports, and renders HTML, knitr
  (R execution), and PDF documents offline. Its bundled deno cache is
  redirected into the sandbox tmp via `DENO_DIR`, and it carries a scoped
  whole-`/proc` read grant because the Dart VM in quarto's SASS compiler
  aborts without `/proc/self/maps` — a documented tradeoff no other profile
  makes.
- **Capability micro-profiles: `git`, `node`, `rust`, `go`, `terminal`.**
  Small built-in profiles that answer one tool's location questions portably
  (binaries via `which:`/`pkg:`, configuration, caches) plus the environment
  variables an interactive terminal program expects, meant to be assembled
  through `inherits` or combined with `--profile tool,git,rust`.
- **Profile smoke-test harness.** A table-driven verification suite
  (`internal/integration/profile_smoke_test.go`) proves each shipped profile
  can run its tool — including `cargo run --offline`, `go run`, a knitr
  render, and a network-denial probe; CI runs it on Ubuntu, macOS, and Nix.
  A profile authoring guide with the slot checklist and house rules ships in
  the website docs.

#### Portable profiles

- **Path entries adapt to the machine.** A profile written on one machine
  works on another:
    - `?path` marks a writable entry optional (skip when missing); `+path/`
      creates a missing directory and `+path` a missing file. Read-only
      entries keep their existing skip-if-missing behavior. The built-in
      agent profiles now create their state directories on first run instead
      of failing on a fresh machine.
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
- **Tool resolvers: `TOOL:ASPECT` path entries.** A grant can ask a tool
  where its own directories are instead of hard-coding them: `r:home`,
  `r:libs`, `uv:cache`, `go:modcache`, `npm:cache`, `tex:dist`, and friends.
  Unlike `which:`/`pkg:` these name ordinary directories and are valid in
  every list, support the `?` and `+` markers, and are re-resolved as literal
  paths so the symlink and sensitive-target checks apply. The set is a fixed
  registry in `bulle`; a profile can never supply a command to run. Unknown
  namespaces are an error rather than a literal path, and prefix-shaped
  aspects drop results that would be a system root. Motivated by R's package
  search path, which holds one entry per installed package (70 on a Nix
  machine) and cannot be written down. `--list-resolvers` prints every
  resolver with what it resolves to on the current machine, so a resolver
  returning nothing is distinguishable from one that works.
- **Profile `env` values expand path variables.** An explicit value in a
  profile's `env` list (`"DENO_DIR=$TMP/bulle/tmp/deno"`) resolves `$TMP`,
  `$HOME`, `$CONFIG`-style variables, so a profile can redirect a tool's
  cache into a sandbox-owned directory portably. `--env` flags and
  `--env-file` contents pass through verbatim.
- **Exec-chain-aware library discovery.** With `add_libs`, bulle scans the
  granted trees for ELF objects (exec trees fully, read-only trees under
  `libs/` directories), follows wrapper scripts' shebang interpreters and
  package-store references, and grants the combined `DT_NEEDED`/`RPATH`
  closure with package stores trusted as RPATH roots. This makes
  interpreters reached through several exec hops (Nix wrappers, version
  managers) work without manual grants. It also follows symlinks between
  store items (gcc's `libgcc_s` living in a separate output), `nix-support`
  flag files (propagated link-time inputs), and store paths baked into the
  dynamic loader itself (Nix patches ld.so with a default libgcc directory
  used for lazy unwinder loads that appear in no ELF header). The scan is
  budgeted, and a truncated scan is reported as a policy note.

#### Diagnostics

- **Failed runs explain what the sandbox blocked.** After a sandboxed
  command fails, `bulle` reads the operating system's own record of sandbox
  denials and prints copy-pasteable fixes, e.g.
  `denied: read /home/user/.gitconfig — add --ro ~/.gitconfig`. On Linux
  this uses Landlock audit records (kernel 6.15+ with the audit subsystem
  enabled); on macOS it queries the unified log, which needs no setup. Hints
  are best-effort and print nothing when denial records are unavailable.
  Denials inside one Nix store item or Homebrew keg collapse into a single
  suggested grant for the package root, so following the hints converges in
  one step instead of one file at a time. See the new *Denial Diagnostics*
  documentation page, including one-line setup instructions per Linux
  distribution.
- **Rerun with an added grant: `--last`.** Every real run records its
  invocation (arguments and working directory) under the user state
  directory. `bulle --last` repeats it from any shell, inserting extra flags
  before the command, and denial diagnostics end with a copy-pasteable
  `bulle: retry with these grants: bulle --last --ro ...` line. The sandbox
  restarts rather than widens.
- **Resolution table in `--policy`.** The policy summary now ends with one
  line per configured entry showing its outcome (granted, skipped, created,
  resolver expansion), and flags entries whose grants collapse into a
  stronger permission on the current platform. `--policy` also works without
  a command: command-dependent grants (`add_exec`, shebang discovery) are
  simply absent from the output.

#### Command-line conveniences

- **Profile inference: `bulle -- claude` selects the matching profile.** When
  no `--profile` is given and the command cannot run under the default
  profile, `bulle` checks whether exactly one installed profile declares that
  command as its `default_app`, selects it, and announces the choice on
  stderr. Inference only rescues runs that would otherwise fail command
  discovery, never overrides an explicit `--profile`, and refuses to guess
  when several profiles match.
- **Configuration defaults: `[defaults]` block.** `config.toml` can supply
  `profile`, `timeout`, `env`, and path grants used when the corresponding
  flag is absent, so bare `bulle` does the usual thing in a repository.
  Explicit flags win; `--no-defaults` ignores the block.
- **The `--` separator may be omitted when unambiguous.** The first
  positional that is an existing directory reads as the workspace; the first
  positional that is not starts the command, announced on stderr. Ambiguity
  resolves toward the workspace, and `--` remains the explicit override.
- **Environment conveniences.** `--env 'GIT_*'` name globs against the
  parent environment (also in profile `env` lists), `--env-file PATH` for
  dotenv-style files, and `--env-all-except NAME,...` for the whole parent
  environment minus named secrets.

### Changed

- **Verbs are subcommands now; the verb-flags are gone.** `bulle policy`
  (with `--json`) replaces `--policy`/`--policy=json`, `bulle rerun` replaces
  `--last`, `bulle profiles list` replaces `--list-profiles`,
  `bulle profiles install SOURCE` replaces `--install-profiles`, and
  `bulle resolvers` replaces `--list-resolvers`. The run itself stays bare —
  `bulle --profile claude ~/project` is unchanged, and everything that
  modifies a run (grants, env, `--timeout`, `--scratch`) remains a flag.
  `policy` accepts every run flag; `rerun` still inserts extra flags before
  the recorded command, and denial hints now suggest `bulle rerun --ro ...`.
  Subcommand names are reserved as the first argument; a workspace with such
  a name is reachable as `./policy` or after `--`.
- **`deny = ["network"]` no longer blocks `socketpair(2)` or the
  `send*`/`recv*` syscalls on Linux.** A socketpair is a connected
  in-process pipe (Linux only supports `AF_UNIX` pairs) and cannot reach
  outside the sandbox, while async runtimes (tokio, libuv) and
  signal-handling crates (signal-hook, used by deno) need one before doing
  any real work. With `socket` and `connect` denied the only sockets a
  process can hold are connected socketpair ends, and addressed sends on a
  connected `AF_UNIX` socket fail with `EISCONN`, so the send/recv family
  cannot reach the abstract namespace either. `socket`, `connect`, `bind`,
  `listen`, and `accept` remain denied.
- **The `tool` profile grants common runtime probes.** `/bin/sh`, `bash`,
  and the coreutils staples launcher scripts call by bare name (`env`,
  `uname`, `sed`, `dirname`, `basename`, `rm`), `/dev/null` and `/dev/tty`
  (rw), and read-only `/proc/stat`,
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
  `target/`, the cargo global cache, git config for vcs metadata). The
  `node` and `go` profiles ask `npm` and `go env` for their cache locations,
  with the previous literal paths as fallbacks.
- **Agent profiles assemble from capability profiles.** `claude`, `codex`,
  `opencode`, and `pi` now inherit `terminal`, `git`, and `node` rather than
  restating environment lists, so they carry git configuration and the Node
  toolchain that agents routinely shell out to. Codex's state directory
  follows `${CODEX_HOME:-$HOME/.codex}`.
- **All runs are now supervised.** Runs without `--timeout` previously
  replaced the `bulle` process with the sandboxed command directly; every run
  now keeps a lightweight parent process, which unifies signal forwarding and
  terminal handling across timed and untimed runs and makes post-run denial
  diagnostics possible. Exit codes and signal behavior are unchanged.
- Upgraded `go-landlock` to v0.9.0 to enable Landlock audit logging of
  sandbox denials on supporting kernels. Enforcement semantics are unchanged.

### Fixed

- **Markers survive resolver expansion.** `?` and `+` on a resolver entry
  were dropped when it expanded, so an optional resolver whose directory did
  not yet exist became a hard failure in a `rw` list.

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
