# TODO

## Near-term features

- [x] Add network policy controls.
  - Do not add a general `--network MODE` CLI flag; network mode is configured through config and profiles.
  - Initial modes:
    - `full`: current behavior; unrestricted network access.
    - `none`: deny network access.
  - Default to `full` for compatibility until a major pre-1.0 policy change is intentional and documented.
  - Add `network = "full"` or `network = "none"` to top-level config and profiles.
  - Merge semantics: profile config overrides inherited profile config; top-level config applies after the selected profile; CLI `--no-network` overrides config to `network = "none"`.
  - Include `network` in `--policy` JSON and profile permission summaries.
  - Add `--no-network` as the only network CLI flag, equivalent to `network = "none"` for that invocation.
  - Do not add `--allow-network`; users can override a stricter profile by choosing or defining a profile with `network = "full"`.
  - Do not add `--add-network`: `add_*` flags currently mean "discover and append supporting filesystem grants", but network is a sandbox mode rather than a discovered grant.
  - macOS Seatbelt:
    - For `full`, keep emitting `(allow network*)`.
    - For `none`, omit `(allow network*)` from the default-deny profile.
  - Linux:
    - Implement `network = "none"` with a seccomp filter that denies socket-related system calls before `exec`.
    - Landlock network rules only cover TCP bind/connect by port on kernels with Landlock ABI v4 or newer; keep them as a possible future allowlist mechanism, not the core `none` implementation.
    - A partial TCP-only implementation can be added later, but it should use an explicit mode name such as `tcp-none` rather than pretending to block all network traffic.
  - Document remaining caveats, including inherited stdio connected to sockets and platform-specific enforcement limits.

- [ ] Add a human-readable policy explainer.
  - Keep `--policy` as stable JSON output.
  - Add a command or flag such as `bulle explain` or `--explain`.
  - Show why each grant exists: workspace, CLI flag, profile, inherited profile, platform runtime root, `--add-exec`, or `--add-libs`.
  - Include environment variables by name only, not value.

- [ ] Add profile management commands.
  - `bulle profiles list`
  - `bulle profiles show NAME`
  - `bulle config path`
  - `bulle init` to write a starter user config.

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

- [ ] Add cross-platform CI coverage.
  - Keep Linux release tests on Ubuntu.
  - Add macOS tests for Seatbelt behavior where GitHub-hosted runners support it.

- [ ] Expand documentation with a threat model page.
  - Clarify what filesystem, environment, network, and process controls can and cannot protect.
  - Include practical recipes for coding agents, package installs, and one-off commands.
