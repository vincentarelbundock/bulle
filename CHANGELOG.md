# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
