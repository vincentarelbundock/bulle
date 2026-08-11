# TODO

- [ ] Add resource and time limits.
  - Support timeouts first, then consider CPU, memory, and disk limits.
  - Expect backend changes because the Linux backend currently `exec`s into the target process.
  - Document platform differences clearly.

- [ ] Document `--scratch` in the user docs.
  - Name the composition explicitly: for parallel trusted sessions, use a
    worktree manager (`wt`-style tools) around bulle; for reviewable untrusted
    runs, use `--scratch`.
  - Worktree integration is deliberately not offered: worktrees share the
    origin's `.git` (including hooks) and need write access to it, which
    defeats scratch isolation. Scratch uses `git clone --local` instead.

- [ ] Consider `bulle scratch list` / `gc` if forgotten scratches accumulate.
  - v1 ships without subcommands; kept scratches live under
    `$XDG_STATE_HOME/bulle/scratch/` with a `meta.toml` beside each, so
    future tooling needs no format migration.

- [ ] Consider an `r-build` profile for source compilation.
  - `install.packages()` from source needs a C/Fortran toolchain, `$R_HOME/etc/Makeconf`, and headers.
  - Much larger grant than the binary installation `r-install` targets; specify only after `r-install` has been exercised in the wild.

- [ ] Decide whether a bare `python` profile (no uv) is worth shipping.
  - System python layouts vary widely and some machines have no `python3` at all.
  - Could be a thin alias selecting `uv` plus a bare-interpreter fallback.

- [ ] Add record mode for profile authoring.
  - Run a command once under observation and emit a minimal profile from what it touched.
  - Use Seatbelt `(trace)` with `sandbox-simplify` on macOS, and `fanotify` or `ptrace` on Linux.
  - Generalize literal hits into `which:`, platform-neutral directory variables, and globs instead of dumping raw paths.
  - Make recording explicit and loud, and prefer scoping it to an existing profile, because a recording run needs more access than the profile it produces.

## Deferred

- [ ] Add macOS Mach-O dependency discovery.
  - Use Go's `debug/macho` to discover dynamic library dependencies for `--add-libs`.
  - Reduce reliance on broad Homebrew and system library roots.
  - Keep fallback behavior for unusual dynamic loader cases.

