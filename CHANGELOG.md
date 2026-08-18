# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

#### A new, first-principles CLI grammar (breaking)

- **One sentence explains every invocation: everything before `--` is
  policy; everything after `--` is the command.** The run grammar is now
  `bulle <profile>[,profile...] [dir] [-- command [args...]]`. The profile is
  the first positional (`bulle claude`, `bulle claude,offline ~/repos/x`);
  the `-p/--profile` flag is gone. The workspace directory is strictly the
  second positional, and a command only ever starts after an explicit `--` —
  the directory-sniffing separator inference (and its stderr announcement)
  is deleted. Errors teach the grammar: a directory in the profile slot, a
  command name without `--`, and a support profile with no default app each
  get a message naming the correct spelling.
- **`bulle -- cmd` just works without selecting a privileged profile.** A
  command given explicitly after `--` always gets its own binary and runtime
  libraries granted, so `bulle -- pandoc doc.md` runs under the minimal
  default sandbox with nothing granted by hand. The `--add-exec` and
  `--add-libs` flags are gone. Profile selection is always explicit: a
  repository-controlled executable named `codex` or `claude` cannot acquire
  the matching agent profile's credentials or writable state by basename.
- **Denied runs end with an offer to save the fix.** On a terminal, a run
  that hit sandbox denials ends by listing the grants that would allow them
  and offering to save them to the profile — `[s]ave and run again`,
  `[w]rite and quit`, or `[n]o`. Saves write a bulle-managed file under
  `<config>/profiles/<name>.toml` (generalized entries, every one optional,
  merged into the same-named profile at load time; hand-written files are
  never rewritten). A run with no profile offers to *create* a profile named
  after the command, with its `default_app` set — that is how profiles are
  born. Non-interactive runs keep the printed denial hints.
- **Fewer subcommands.** `policy`, `resolvers`, `profiles list`, and
  `config` are folded into one inspection command: `bulle show
  [policy|profiles|resolvers|config]` (bare `show` prints the policy;
  `--json` still applies). `record` is gone — the save prompt subsumes it
  interactively, one reviewed round at a time. `rerun` is gone — shell
  history plus the save prompt cover it, and the last-run state file is no
  longer written. What remains: `scratch`, `show`, `profiles install`,
  `completion`, `help`, `version`.
- **Help is short and layered.** The front help screen is ~45 lines: the
  grammar, the four grant flags, `--env`, and the subcommands. The advanced
  material moved to `bulle help grants|env|limits|config`, and the profile
  listing to `bulle show profiles`. Completion now offers profiles and
  subcommands together in the first position.
- **The git profile has `default_app = "git"`**, so `bulle git` runs git and
  a bare app-less profile invocation is rarer.

### Added

#### Man page

- **`bulle __man` prints a bulle.1 man page**, assembled from the same
  strings the terminal help prints — the front usage screen, the subcommand
  help, and the help topics — so it cannot drift from the CLI, and a test
  asserts every section appears. Release archives ship `man/bulle.1`
  (goreleaser runs `scripts/generate-man.sh`), `make install` places it
  under `$(PREFIX)/share/man/man1`, and the Homebrew formula writes it by
  asking the built binary.

#### Shell completions

- **`bulle completion bash|zsh|fish` prints a completion script.** The
  scripts are thin shims: at completion time they call `bulle __complete` on
  the installed binary, which answers with subcommand names, verbs, flags,
  and positional profile names — including profiles installed later under
  `~/.config/bulle/profiles/`, and comma-separated profile merges. Because
  the binary answers, an installed script never goes stale.
- **Completions ship with releases.** Release archives include generated
  `completions/` files (goreleaser runs `scripts/generate-completions.sh`, so
  the archived scripts are exactly what that version's binary prints), the
  Homebrew formula installs them via `generate_completions_from_executable`,
  and `make install` places them under `$(PREFIX)/share` for bash, zsh, and
  fish. Because the shims query the binary at completion time, an installed
  script keeps working across upgrades either way.
- **Completion cannot drift from the CLI.** Flag completion is derived by
  reflection from the same `Flags` struct the parser reads, and subcommand
  completion reads the same table `Run` dispatches from.

#### Per-subcommand help

- **Every subcommand answers `--help`.** `bulle scratch --help`, `bulle
  show -h`, and the rest print focused help for that subcommand instead of
  a one-line usage error; `bulle help <subcommand>` prints the same text, and
  `bulle help <TAB>` completes the topic names. A `--help` after the `--`
  separator still belongs to the sandboxed command. A test asserts every
  dispatched subcommand has a help topic, so the two cannot drift.
- **Bare invocations that have nothing to do print help.** `bulle` alone
  (when no configured default_app gives it something to run), and bare
  `profiles` and `completion` — which cannot mean anything without arguments
  — show help and exit 0 instead of a terse error. Bare `show` and `scratch`
  keep their meaningful behavior.

#### Friendlier errors and `bulle show config`

- **Misspelled profile names get a suggestion.** `bulle claud` now says
  `did you mean "claude"?` (drawn from every profile in scope, user-installed
  ones included); with no near miss, the error points at
  `bulle show profiles`.
- **Flag errors point at the flag, not the whole help.** A rejected flag
  value shows that flag's own syntax line (`usage: --memory SIZE ...`)
  instead of referring to the 200-line help; unknown flags keep kong's
  suggestion or fall back to one of ours.
- **`bulle show config` reports the configuration in effect.** It prints the
  configuration root, whether `config.toml` and `profiles/` were found and
  load cleanly, and the built-in profile count. Runs deliberately ignore a
  missing `config.toml`, so this is where a mistyped directory or a broken
  file becomes visible; a load error exits non-zero.

### Changed

- Subcommand dispatch moved from a literal switch to a declared table
  (`internal/app/commands.go`) shared with completion; `bulle help` and
  `bulle version` dispatch through it as well, with unchanged behavior.

#### Graphical applications

- **A `gui` profile.** Graphical programs need a display connection before they
  need anything else: the session's environment variables, the compositor
  socket under `$XDG_RUNTIME_DIR`, `/tmp/.X11-unix` for X11 and XWayland,
  `/dev/dri` and `/dev/shm` for rendering, fontconfig's configuration, and
  writable shader and font caches. On macOS the profile carries only the font
  directories: a process there reaches the window server over Mach IPC, and the
  service list was deliberately not guessed, since a wrong one would look like
  support and fail silently — mach-lookup denials are filtered out of denial
  hints as noise.
- **`$XDG_RUNTIME_DIR` is available to profiles.** The desktop session's
  sockets live there, so without it a graphical profile has to hardcode
  `/run/user/<uid>` and stops being portable. It has no fallback, unlike the
  other XDG variables: there is no sensible default under `$HOME`, so entries
  referring to it should be optional.
- **Some font locations cannot be written down.** Distributions that assemble
  `/etc` from a package store reach fontconfig's configuration through symlinks
  a directory grant does not follow, which the profile handles by globbing; but
  NixOS also writes absolute store paths for each font package into its
  generated fontconfig files, and those hashes differ per machine. Run the
  application with `bulle gui -- <app>`, review its denial hints, and add only
  the required grants. A denied font tree is not fatal — text renders in the
  wrong typeface — so it is easy to miss.

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
  behind each limit in `bulle show` output. `--strict-limits` turns the
  warning into a refusal to run, for continuous integration.
- **Timeouts require a container that can kill the whole process tree.** On
  Linux, every nonzero timeout creates a delegated cgroup and terminates via
  `cgroup.kill`, including descendants that called `setsid`; the cgroup is
  also emptied when the leader exits. The run fails before exec if that
  guarantee is unavailable. macOS has no equivalent unprivileged primitive,
  so nonzero timeouts fail closed there instead of promising an escapable
  process-group timeout.

#### Workspaces

- **`bulle scratch` disposable workspaces.** Run the sandboxed command against
  a throwaway local clone of the workspace instead of the real checkout. The
  clone carries uncommitted work (tracked changes and untracked files) so the
  agent starts from the state you see; git objects are copied with
  `--no-hardlinks`, so a writable scratch cannot mutate an inode shared with
  the origin, and the real checkout is never granted to the sandbox.
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
  (`bulle scratch claude`), with `--` available for a command named
  like a verb and a typo guard so a mistyped verb reports itself instead of
  cloning. The flag form `--scratch` remains and composes with `bulle show`.
  `--scratch-keep` skips the prompt for non-interactive
  runs, `[scratch] dir` in `config.toml` relocates scratches, and denial hints from scratch runs are
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
  runs add the `user-bin` profile when a user-installed helper is denied.
- **Built-in `r`, `r-install`, `uv`, and `uv-install` profiles.**
  `bulle r -- Rscript analysis.R` and
  `bulle uv -- uv run script.py` work on conventional installs
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
  through `inherits` or combined as `bulle tool,git,rust -- cargo build`.
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
  enabled) and a deliberate marker denial to bind records to this run's
  Landlock domain, so concurrent sandboxes cannot contaminate the result; on
  macOS it queries the unified log, which needs no setup. Hints are
  best-effort and print nothing when denial records are unavailable.
  Denials inside one Nix store item or Homebrew keg collapse into a single
  suggested grant for the package root, so following the hints converges in
  one step instead of one file at a time. See the new *Denial Diagnostics*
  documentation page, including one-line setup instructions per Linux
  distribution.
- **Resolution table in `bulle show`.** The policy summary now ends with one
  line per configured entry showing its outcome (granted, skipped, created,
  resolver expansion), and flags entries whose grants collapse into a
  stronger permission on the current platform. Inspection also works without
  a command: command-dependent grants are simply absent from the output.

#### Command-line conveniences

- **Profiles are selected explicitly.** The profile is the first positional
  argument, and an explicit command begins only after `--`. Executable
  basenames are never treated as authority to load a profile.
- **Configuration defaults: `[defaults]` block.** `config.toml` can supply
  `profile`, `timeout`, `env`, and path grants used when the corresponding
  flag is absent, so bare `bulle` does the usual thing in a repository.
  Explicit flags win; `--no-defaults` ignores the block.
- **The `--` separator is mandatory before a command.** This keeps a profile,
  workspace directory, and command syntactically distinct; no filesystem
  lookup or heuristic changes the parse.
- **Environment conveniences.** `--env 'GIT_*'` name globs against the
  parent environment (also in profile `env` lists), `--env-file PATH` for
  dotenv-style files, and `--env-all-except NAME,...` for the whole parent
  environment minus named secrets.

### Changed

- **Denial hints collapse per-process `/proc` entries into one suggestion.** A
  denied `/proc/1234/cgroup` used to produce a hint naming that path, which was
  useless twice over: the pid belongs to a process that has already exited, and
  a tool that reads `/proc/self/...` in each of its children produced one hint
  per child, spending the ten-hint display budget on a single underlying grant.
  Such denials now report `/proc`, matching the grant that actually covers them
  — `/proc/self` does not, because it resolves at grant time to the granting
  process rather than the child that hit the denial, which is why the `quarto`
  profile already grants `/proc` whole. Note that this grant lets the sandboxed
  command read other same-uid processes' `/proc` entries; the recorded profile
  entry says so, and the hint is a suggestion you review before applying.
- **Inspection verbs are consolidated under `bulle show`.** `show policy`
  (the default), `show profiles`, `show resolvers`, and `show config` share a
  single non-executing entry point; obsolete `record` and `rerun` stateful
  workflows are removed.
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
- **An optional `pkg:` entry degrades instead of failing the run.** A `?pkg:`
  entry whose package root is a system directory — a distribution's
  `/usr/bin/git`, whose root would be `/usr` — aborted the whole run, though
  the `?` marker exists to say "grant this where it makes sense". It now grants
  the binary and its symlink chain and drops the root, which is what git.toml's
  own comment already claimed happened. Without the marker the refusal stands,
  since the author asked for the package tree.
- **Grant coverage compares resolved paths on both sides.** A path was
  resolved through symlinks and then tested against roots that were not, so a
  directory reached through a symlink covered nothing under it: on macOS, where
  every temporary directory is `/var/…` resolving to `/private/var/…`, the
  shim directory was silently dropped from `PATH`. A path whose resolved form
  escapes every root is still refused.
- **`--json` after `--` belongs to the command.** `bulle show policy … -- curl
  --json …` no longer has that flag stolen from the command line it reports.
- **Parse errors name the flag that failed**, not the longest flag name
  appearing anywhere in the message — including one that was only a rejected
  value.

### Security

Following a whole-codebase audit. The through-line of the serious findings is
one mistake in several places: content the confined process or the untrusted
workspace controls flowed into a decision bulle makes *outside* the sandbox.

- **The scratch's own `.git` can no longer drive code execution after the
  run.** A `--scratch` workspace is granted read-write, `.git/` included, and
  the review gate then runs `git add -A` as the user with the sandbox gone —
  so a `filter.<name>.clean`, `core.pager`, `core.hooksPath`, `diff.external`
  or `uploadpack.packObjectsHook` written during the run executed unsandboxed,
  before the review prompt was even printed. The configuration git wrote at
  clone time is snapshotted outside the workspace and restored before any
  post-run git command, hooks are cleared, and every scratch-directed git
  invocation additionally passes `-c` overrides for the exec-capable settings.
- **Tool resolvers no longer run interpreters in the workspace.** `r:*`,
  `npm:cache`, `go:*` and friends ran with the current directory as cwd, so a
  repository's `.Rprofile` or `.npmrc` steered the answer — and, for R, ran
  code — before any sandbox existed. Resolvers now run from `/`, and `Rscript`
  is invoked with `--no-init-file --no-site-file`.
- **Resolver output is guarded for every entry, not one.** The system-root and
  home refusal applied only to `r:prefix`; a tool persuaded to report `$HOME`
  (or an ancestor of it) had that granted verbatim.
- **Path variables are expanded exactly once.** An entry was expanded, then
  expanded again, so a `$` arriving *inside* an environment value was
  reinterpreted as a further reference — turning a value that passed
  validation into `/` or `$HOME`. Fallbacks (`${CODEX_HOME:-$HOME/.codex}`)
  still resolve, because a fallback is profile text rather than data. An entry
  that becomes the filesystem root or the home directory only through
  expansion is refused, and variable values containing glob metacharacters or
  a dollar sign are rejected.
- **Linux: the seccomp network filter checks `seccomp_data.arch`,** so a
  32-bit or x32 process can neither slip past `network = none` (i386 `socket`
  is 359, not 41) nor be hit by it (i386 41 is `dup`); a foreign ABI is killed.
  The filter is also installed on a pinned OS thread and the exec happens on
  that same thread — `PR_SET_SECCOMP` is per-thread, so the runtime was free to
  exec from a thread that never received it, leaving the network fully open
  while bulle reported it offline.
- **`add_libs` no longer scans writable executable trees.** A shebang or Nix
  store reference found under an `rwx` grant is written by the confined
  process, so following it let a run choose what the next run grants
  `FS_EXECUTE` on. Only read-only executable roots are scanned now.
- **Symlink grants refuse ancestors of the home directory,** not just the home
  directory itself: `/home` and `/Users` hold every other user's keys.
- **A `..` component that crosses a symlink grants only what the kernel
  reaches.** `x/link/../b` was cleaned lexically to `x/b` and both were
  granted, so a directory the entry never traverses came along.
- **cgroup limits fail closed.** Support was probed by creating a directory,
  which does not test whether the controllers can be delegated — a parent
  holding processes of its own can never gain one. The run then swallowed the
  creation failure, so `memory: 4G (cgroup v2)` and `--strict-limits` both
  passed over a run with no cap at all. Detection now checks delegation, and a
  cgroup that could not be created after being reported as enforced is a hard
  failure. Each run's cgroup is uniquely named (pids are recycled) and stale
  empty ones are swept.
- **The permission summary no longer prints `none` over a real grant.** A path
  held both `rw` and `rox` was treated as subsumed by "the other" list and
  dropped from both. Policy summaries are inspection output only and are no
  longer pasted into an agent prompt or command input.
- **Learned grants: a much higher promotion floor, and a prompt that shows
  what is written.** Three denied files promoted to a grant on the directory
  holding them with only one component below `$HOME` or `/`, so
  `~/.ssh/known_hosts` and two harmless siblings produced a grant on every
  private key you own. Directories one level below `$HOME` or a filesystem
  root no longer promote, credential directories never do, and a home is
  recognized by location rather than by whether `$HOME` happens to spell it.
  The save prompt now lists the generalized, promoted entries the file will
  receive rather than the literal per-file grants.
- **`--env-all-except` no longer overrides values a profile set.** Bulk
  passthrough silently undid profile hardening such as uv's `UV_NO_CACHE=1`.
  Explicit `--env` and `--env-file` values still win.
- **`profiles install` is neither silent nor destructive.** It prints what
  each file grants, warns when a file merges into a built-in profile (and so
  into everything inheriting from it), and refuses to replace an already
  installed profile without `--force`.
- **Saved profiles keep hand-written keys.** A rewrite modeled only the grant
  lists, so a `deny = ["network"]` added by hand — exactly what the file's own
  header invites — was silently dropped on the next save.
- **macOS: the Seatbelt log pattern is anchored,** so a filename containing a
  newline cannot inject a fabricated denial (and therefore a suggested grant)
  into the parser.
- **The terminal is only handed to the child when bulle holds it.** Backgrounded
  (`bulle -- cmd &`), bulle took the foreground from the shell, sending the
  user's keystrokes to the sandboxed command.
- **Signal forwarding probes before it signals** and stops once the forwarder
  has been shut down, so a buffered signal cannot be delivered to a recycled
  process group after the run has returned.
- **Learned grants no longer record scratch paths.** A denial under `--scratch`
  wrote a per-run scratch path into a permanent profile; those paths are mapped
  back to the origin workspace first.
- **macOS: the fd sweep honors `RLIMIT_NOFILE`.** The fallback stopped at 1023
  even where the limit had been raised, leaking inherited descriptors past the
  sandbox profile — the Linux fallback already did this.
- **Copy-pasteable scratch commands are shell-quoted,** so a workspace whose
  name contains a space cannot turn a printed `rm -rf` into two arguments.

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
