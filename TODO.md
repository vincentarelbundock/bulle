# TODO

## Near-term features

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

