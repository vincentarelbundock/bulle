# TODO

- [x] Add resource and time limits.
  - `--timeout`, then `--memory`, `--cpu`, `--nproc` (cgroup v2, Linux only) and
    `--nofile`, `--fsize`, `--cpu-time` (rlimits, portable).
  - Unenforceable limits warn on stderr; `--strict-limits` refuses to run instead.

- [ ] Consider an `r-build` profile for source compilation.
  - `install.packages()` from source needs a C/Fortran toolchain, `$R_HOME/etc/Makeconf`, and headers.
  - Much larger grant than the binary installation `r-install` targets; specify only after `r-install` has been exercised in the wild.

- [ ] Decide whether a bare `python` profile (no uv) is worth shipping.
  - System python layouts vary widely and some machines have no `python3` at all.
  - Could be a thin alias selecting `uv` plus a bare-interpreter fallback.

- [x] Add record mode for profile authoring (Linux).
  - `bulle record --profile <base> -- <command>` iterates: run, collect denials, add grants, repeat.
  - Iteration replaced the planned `fanotify`/`ptrace` observation: each round runs under a sandbox no wider than the base plus what it has already been denied, so recording never needs the elevated grant that tracing would.
  - Entries are generalized to `which:`, tool resolvers, and directory variables; clusters collapse to directory grants; variable roots are never granted.

- [ ] Extend record mode to macOS.
  - Seatbelt violations are already parsed, and `grantForSeatbeltDenial` maps them to entries.
  - The blocker is noise: macOS denies many benign probes the Landlock path never sees, so a recorded profile would be far wider than the run needs. Needs a filter before it can be honest.
  - Consider `(trace)` with `sandbox-simplify` as an alternative source.

- [ ] Consider zero-to-profile recording (no `--profile`).
  - Deferred deliberately: the recording grant is widest and the output least trustworthy with no base to diff against.

## Deferred

- [ ] Add macOS Mach-O dependency discovery.
  - Use Go's `debug/macho` to discover dynamic library dependencies for `--add-libs`.
  - Reduce reliance on broad Homebrew and system library roots.
  - Keep fallback behavior for unusual dynamic loader cases.

