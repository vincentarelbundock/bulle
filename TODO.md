# TODO

## Near-term features

- [ ] Add ephemeral state support.
  - Provide a mode such as `--ephemeral-home` or `--scratch-profile-state`.
  - Run agents with temporary writable state instead of real app config/cache directories.
  - Make the cleanup behavior explicit and predictable.

- [ ] Add rerun with an added grant.
  - Offer to re-run the same invocation plus the suggested grant after a denial hint.
  - Add `bulle --last` so a fresh shell can repeat and amend the previous invocation.
  - Restart rather than widen: Landlock cannot extend a live sandbox, and agents resume from their own session state.

- [ ] Add defaults to the user configuration file.
  - Support a `[defaults]` block for `profile`, `timeout`, `env`, and path grants.
  - Let explicit flags override the block, with `--no-defaults` to ignore it.
  - Aim for bare `bulle` doing the usual thing in a repository.

- [ ] Allow omitting `--` when the first positional is unambiguous.
  - Treat a first positional that is not an existing directory but names a runnable command as the command.
  - Resolve ambiguity toward the workspace reading and note the choice on stderr.
  - Keep `--` as the explicit way to force the command reading.

- [ ] Ship capability micro-profiles.
  - Add small built-in profiles for `git`, `node`, `python`, `rust`, and `go`.
  - Let agent profiles assemble from them through `inherits` instead of restating tool layouts.
  - Fix per-tool location questions once, in one file, for every profile that inherits it.

- [ ] Add R and Python profiles.
  - Cover R read-execute: `R` and `Rscript`, plus `R_HOME` (`R.home()`), which is a wrapper target outside the binary directory on Nix and Homebrew alike.
  - Cover R libraries: the user library (`R_LIBS_USER`, by default `~/R/<platform>-library/<version>`) read-write, and every entry of `.libPaths()` read-only.
  - Cover R state: `~/.Rprofile`, `~/.Renviron`, and the `tools::R_user_dir` roots `~/.cache/R`, `~/.config/R`, `~/.local/share/R`.
  - Cover `renv`: its root defaults to `~/.cache/R/renv` on Linux but `~/Library/Caches/org.R-project.R/R/renv` on macOS, and `RENV_PATHS_CACHE` accepts a semicolon-separated list. Check whether `renv`'s own system-library sandbox (`RENV_PATHS_SANDBOX`) needs a grant of its own.
  - Note the macOS library split: CRAN builds default `R_LIBS_USER` to `~/Library/R/<machine>/<x.y>/library`, not the Linux `~/R/<platform>-library/<x.y>`.
  - Cover R temporary files: R derives `tempdir()` from `TMPDIR`/`TMP`/`TEMP` at startup and creates a per-session `Rtmp*` directory there, so the sandbox temporary directory must be writable and those variables must be set.
  - Cover Python with `uv`: the `uv` binary, `uv cache dir` (`~/.cache/uv`), `uv tool dir` and `uv python dir` (`~/.local/share/uv/tools`, `~/.local/share/uv/python`), the config directory `~/.config/uv`, the executable directory `~/.local/bin`, and a workspace-local `.venv`.
  - Note that `uv` follows XDG on macOS as well, so it uses `~/.cache` and `~/.local/share` there rather than `~/Library`. Its overrides are `UV_CACHE_DIR`, `UV_TOOL_DIR`, `UV_PYTHON_INSTALL_DIR`, `UV_PYTHON_BIN_DIR`, `UV_TOOL_BIN_DIR`, and `UV_PROJECT_ENVIRONMENT`.
  - Keep interpreter, package manager, and project state separable so a project using only one of them does not inherit the rest.
  - Decide whether installing packages from CRAN or PyPI is in scope, since that needs network access and a writable library path.

- [ ] Resolve interpreter library paths by asking the interpreter.
  - Static path lists cannot express `.libPaths()`: on this machine it holds 70 entries, 69 of them distinct `/nix/store/<hash>-r-<pkg>/library` paths, and the set changes on every package update.
  - Globbing `/nix/store/*/library` would work but grants every R package in the store, including ones not on the search path.
  - Add named resolvers that run a known interpreter before the sandbox starts, such as `r-libs:` for `.libPaths()` and `python-path:` for `sys.path`, generalizing the `which:` resolver.
  - Consider a companion resolver that expands a `PATH`-style environment variable into individual grants, since `R_LIBS_SITE` already carries the full list.
  - Keep resolvers a fixed set implemented in Go against known interpreters, not arbitrary commands named by a profile, and feed every result back through `resolve()` for the symlink and sensitive-target checks.
  - Treat this as the answer to conflicting platform conventions rather than assuming a single mapping: on macOS `uv` uses XDG paths while `renv` uses `~/Library/Caches`, so no platform-neutral directory variable is correct for both. Tools that can report their own directories (`uv cache dir`, `renv::paths$cache()`) should be asked.

- [ ] Add environment flag conveniences.
  - Support glob matching against the parent environment, such as `--env 'GIT_*'`.
  - Add `--env-file PATH` for dotenv-style files.
  - Add `--env-all-except NAME,...` for throwaway commands that want the shell environment minus secrets.

## Medium-term features

- [ ] Add macOS Mach-O dependency discovery.
  - Use Go's `debug/macho` to discover dynamic library dependencies for `--add-libs`.
  - Reduce reliance on broad Homebrew and system library roots.
  - Keep fallback behavior for unusual dynamic loader cases.

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

