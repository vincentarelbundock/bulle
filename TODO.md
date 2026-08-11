# TODO

- [ ] Add resource and time limits.
  - Support timeouts first, then consider CPU, memory, and disk limits.
  - Expect backend changes because the Linux backend currently `exec`s into the target process.
  - Document platform differences clearly.

- [ ] Add git worktree integration.
  - Support `--worktree NAME` to create or reuse a worktree and make it the workspace.
  - Replace the manual create-then-sandbox dance for branch-shaped agent work.
  - Decide how worktree cleanup interacts with the sandboxed run.

- [ ] Add a scratch workspace mode.
  - Run against a throwaway copy or worktree of the repository instead of the real checkout.
  - Offer diff, apply, keep, and discard once the command exits.
  - Coordinate with ephemeral state support so filesystem and app state scratch behave consistently.

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

