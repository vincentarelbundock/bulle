# TODO

## Near-term features

- [ ] Add a human-readable policy explainer.
  - Keep `--policy` as stable JSON output.
  - Add a command or flag such as `bulle explain` or `--explain`.
  - Show why each grant exists: workspace, CLI flag, profile, inherited profile, platform runtime root, `--add-exec`, or `--add-libs`.
  - Include environment variables by name only, not value.

- [ ] Add ephemeral state support.
  - Provide a mode such as `--ephemeral-home` or `--scratch-profile-state`.
  - Run agents with temporary writable state instead of real app config/cache directories.
  - Make the cleanup behavior explicit and predictable.

## Medium-term features

- [ ] Add macOS Mach-O dependency discovery.
  - Use Go's `debug/macho` to discover dynamic library dependencies for `--add-libs`.
  - Reduce reliance on broad Homebrew and system library roots.
  - Keep fallback behavior for unusual dynamic loader cases.

- [ ] Add resource and time limits.
  - Support timeouts first, then consider CPU, memory, and disk limits.
  - Expect backend changes because the Linux backend currently `exec`s into the target process.
  - Document platform differences clearly.

## Supporting work

- [x] Add cross-platform CI coverage.
  - Keep Linux release tests on Ubuntu.
  - Add macOS tests for Seatbelt behavior where GitHub-hosted runners support it.

- [ ] Expand documentation with a threat model page.
  - Clarify what filesystem, environment, network, and process controls can and cannot protect.
  - Include practical recipes for coding agents, package installs, and one-off commands.
