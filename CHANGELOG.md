# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.0.7]: https://github.com/vincentarelbundock/bulle/compare/v0.0.6...v0.0.7
