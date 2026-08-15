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

- [x] Extend record mode to macOS.
  - The feared blocker (benign startup probes) turned out to be handled already: those paths are in `tool.toml`, so the coverage filter drops them.
  - The real limitation is attribution, and it is structural. The unified log reports violations from every sandboxed process, and a violation names an exited pid, so it cannot be tied to the run's process group. Recording annotates each entry with the denied process rather than guessing — filtering by name would drop a command's helpers.
  - **Unverified on real hardware.** Written and cross-compiled on Linux; needs a run on a Mac before the macOS path is trusted.

- [ ] Consider `(trace)` with `sandbox-simplify` as a second macOS source.
  - Would give per-run attribution the unified log cannot, at the cost of a separate mechanism and output format.

- [ ] Consider zero-to-profile recording (no `--profile`).
  - Deferred deliberately: the recording grant is widest and the output least trustworthy with no base to diff against.

## Deferred

- [ ] Add macOS Mach-O dependency discovery.
  - Use Go's `debug/macho` to discover dynamic library dependencies for `--add-libs`.
  - Reduce reliance on broad Homebrew and system library roots.
  - Keep fallback behavior for unusual dynamic loader cases.

