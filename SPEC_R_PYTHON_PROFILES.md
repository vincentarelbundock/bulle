# SPEC: R and Python (uv) profiles

Status: draft. Scope: built-in profiles that let a sandboxed command run R and
Python code, and optionally install packages.

## 1. Goals

- `bulle --profile r -- Rscript analysis.R` works on a conventional R install
  without hand-written grants.
- `bulle --profile uv -- uv run script.py` works the same way.
- An agent profile can inherit either without restating interpreter layout.
- The same profile text works on Debian, Homebrew, and Nix, and on both
  supported platforms.

## 2. Non-goals

- System package managers (`apt`, `brew`) inside the sandbox.
- Conda, Anaconda, and `renv`.
- IDE integration (RStudio, Positron, Jupyter kernels).
- Compiling R packages from source is out of scope for the base profiles; see
  §7 for why it is separated.

## 3. Why the existing mechanisms are not enough

The current profile format resolves literal path strings against `$HOME`,
`$WORKSPACE`, `$TMP`, and `$TMPDIR`. Three properties of R and Python defeat
that.

**Interpreter location is not predictable.** On this machine `R` on `PATH` is
`/etc/profiles/per-user/vincent/bin/R`, resolving to a wrapper at
`/nix/store/bg5pcra…-R-4.5.3-wrapper/bin/R`, while `R_HOME` is a different store
path entirely (`/nix/store/k9xnrrl…-R-4.5.3/lib/R`). Nothing about the binary's
directory predicts where R's own files live.

**The library search path is dynamic and unbounded.** `.libPaths()` here returns
70 entries: one user library plus 69 distinct `/nix/store/<hash>-r-<pkg>/library`
paths. The set changes on every package update. A glob such as
`/nix/store/*/library` would match, but grants every R package in the store
rather than the ones actually on the search path.

**Runtime library dependencies are not discoverable from the executable.**
`ldd` on `Rscript` reports only `libc`. But `Amelia.so`, an ordinary installed
package, pulls in `liblapack`, `libblas`, `libgfortran`, `libquadmath`,
`libstdc++`, `libgomp`, `libreadline`, and `libR` — each from a different store
path. These are `dlopen`ed at runtime, so `elfdeps.GetLibraryDependencies` on
the interpreter binary will never find them.

This spec therefore depends on mechanisms tracked separately in `TODO.md`:
`which:` resolution with a shim directory, interpreter-querying resolvers, and
optional grants. §9 defines what can ship before those land.

## 4. Verified facts

Measured on this machine (R 4.5.3, uv 0.11.21, NixOS) unless marked as
documented defaults.

### R

| Item | Value |
| --- | --- |
| `R` on `PATH` | `/etc/profiles/per-user/vincent/bin/R` → wrapper in `/nix/store/…-R-4.5.3-wrapper/bin/R` |
| `R_HOME` | `/nix/store/…-R-4.5.3/lib/R` (separate store path from the wrapper) |
| `.libPaths()` | 70 entries; 1 user library, 69 store paths |
| `R_LIBS_USER` | `~/R/x86_64-pc-linux-gnu-library/4.5` |
| `R_LIBS_SITE` | populated by R at startup, **empty in the parent environment** |
| `tempdir()` | `/tmp/Rtmp<random>`, derived from `TMPDIR`/`TMP`/`TEMP` |
| `R_SESSION_TMPDIR` | set *by* R for child processes; not an input |
| Startup files present | `~/.Rprofile`, `~/.Renviron` |
| `tools::R_user_dir` roots | `~/.cache/R`, `~/.config/R`, `~/.local/share/R` |
| `$R_HOME/etc` contents | `Renviron`, `Makeconf`, `ldpaths`, `repositories`, `javaconf` |

`R_LIBS_SITE` and `R_LIBS_USER` are assigned in `$R_HOME/etc/Renviron` as
`${R_LIBS_SITE:-'%S'}` and `${R_LIBS_USER:-'%U'}`; R substitutes the compiled-in
defaults at startup. **Granting `R_HOME` matters more than passing these
variables through.**

Documented platform default (not measured): CRAN macOS builds set `R_LIBS_USER`
to `~/Library/R/<machine>/<x.y>/library`, not the Linux
`~/R/<platform>-library/<x.y>`.

### uv

| Item | Value | Override |
| --- | --- | --- |
| Cache | `~/.cache/uv` | `UV_CACHE_DIR` |
| Data root | `~/.local/share/uv` | `XDG_DATA_HOME` |
| Tools | `~/.local/share/uv/tools` | `UV_TOOL_DIR` |
| Python installs | `~/.local/share/uv/python` | `UV_PYTHON_INSTALL_DIR` |
| Config | `~/.config/uv` | `XDG_CONFIG_HOME` |
| Executables | `~/.local/bin` | `UV_PYTHON_BIN_DIR`, `UV_TOOL_BIN_DIR` |
| Project venv | `<workspace>/.venv` | `UV_PROJECT_ENVIRONMENT` |

uv follows XDG on **macOS as well as Linux** — it does not use `~/Library`.
This directly conflicts with R, which does use `~/Library` on macOS. No single
platform-neutral directory variable is correct for both; each profile must
encode its own tool's convention, or ask the tool.

`uv run --offline` succeeds against an existing virtual environment (verified:
`uv init` followed by `uv run --offline python -c ...` with network denied at
the uv level). An offline-by-default `uv` profile is therefore workable, though
the first `uv init`/`uv sync` in a project needs `uv-install`.

Python packaging is easier than R here: compiled wheels generally vendor their
shared objects inside the wheel (and `auditwheel`/`delocate` rewrite them to
load from within the package), so the §3 `dlopen` problem is much smaller for
uv-managed environments than for R.

## 5. Profile decomposition

Five profiles, so that a project using one tool does not inherit the other.

| Profile | Grants | Network |
| --- | --- | --- |
| `r` | interpreter, `R_HOME`, all `.libPaths()` entries read-only, startup files, temp | no |
| `r-install` | inherits `r`; user library read-write, CRAN access | yes |
| `uv` | `uv` binary, cache, python installs, tools, config, workspace `.venv` | no |
| `uv-install` | inherits `uv`; cache and venv read-write, PyPI access | yes |
| `python` | thin alias selecting `uv` plus a bare interpreter fallback | no |

The base profiles are **read-only for libraries**. This is the central policy
decision of this spec: a sandboxed agent that can write `R_LIBS_USER` or the uv
cache can overwrite packages that the user's own non-sandboxed sessions will
later load, which turns a sandbox escape from "wrote in the workspace" into
"wrote code that runs outside the sandbox next time." Installation is opt-in
through the `-install` variants.

## 6. Draft profiles

Written against the mechanisms in §3, and therefore not yet installable. `?`
marks an optional grant; `which:` and `r-libs:`/`uv-dirs:` are resolvers.

```toml
# profiles/r.toml
title = "R"
description = "R interpreter, library search path, and startup files"
inherits = "tool"

rox = ["which:R", "which:Rscript"]
ro = [
  "r-home:",           # R.home(), including etc/ and lib/libR.so
  "r-libs:",           # every .libPaths() entry, read-only
  "?~/.Rprofile",
  "?~/.Renviron",
  "?$R_USER_CONFIG/R",
]
env = [
  "HOME", "USER", "LANG", "TERM",
  "R_LIBS", "R_LIBS_USER", "R_LIBS_SITE",
  "R_ENVIRON", "R_ENVIRON_USER", "R_PROFILE", "R_PROFILE_USER", "R_USER",
]
deny = ["network"]
add_libs = true
```

```toml
# profiles/r-install.toml
title = "R (with package installation)"
description = "R plus a writable user library and CRAN access"
inherits = "r"

rw = ["r-libs-user:"]   # R_LIBS_USER only, created if missing
allow = ["network"]
```

```toml
# profiles/uv.toml
title = "uv"
description = "uv-managed Python interpreters and environments"
inherits = "tool"

rox = ["which:uv", "uv-dirs:python", "uv-dirs:tools"]
ro = ["uv-dirs:cache", "?$XDG_CONFIG_HOME/uv"]
rwx = ["?$WORKSPACE/.venv"]
env = [
  "HOME", "USER", "LANG", "TERM",
  "UV_CACHE_DIR", "UV_TOOL_DIR", "UV_PYTHON_INSTALL_DIR",
  "UV_PYTHON_BIN_DIR", "UV_TOOL_BIN_DIR", "UV_PROJECT_ENVIRONMENT",
  "VIRTUAL_ENV",
]
deny = ["network"]
```

```toml
# profiles/uv-install.toml
title = "uv (with package installation)"
description = "uv plus a writable cache and PyPI access"
inherits = "uv"

rwx = ["uv-dirs:cache"]
allow = ["network"]
```

Environment variables are listed so that an explicitly configured value is
honored, but are passed **only when already set** — R and uv compute correct
defaults otherwise, and forcing a value would break the Nix case.

## 7. Compilation is separate

`install.packages()` from source needs a C/Fortran toolchain, `$R_HOME/etc/Makeconf`,
and headers. That is a much larger grant than binary installation and pulls in
compiler paths that vary more than anything else in this spec. `r-install`
targets binary packages; source compilation, if supported at all, belongs in a
separate `r-build` profile and should be specified only after `r-install` works.

## 8. New resolvers required

**Implemented.** The registry lives in `internal/policy/tools.go`; `--list-resolvers`
shows every entry with what it resolves to on the current machine.

| Resolver | Implementation | Output |
| --- | --- | --- |
| `which:NAME` | look up on parent `PATH` | binary, its realpath target, shim entry |
| `r:home` | `Rscript -e 'cat(R.home())'` | one directory |
| `r:prefix` | `Rscript -e 'cat(dirname(dirname(R.home())))'` | one directory, system roots dropped |
| `r:libs` | `Rscript -e 'cat(.libPaths(), sep="\n")'` | N directories |
| `r:libs-user` | `Rscript -e 'cat(Sys.getenv("R_LIBS_USER"))'` | one directory |
| `uv:cache`, `uv:tools`, `uv:python` | `uv cache dir`, `uv tool dir`, `uv python dir` | one directory |

`r:prefix` exists because `R_HOME` is not the installation root: on this machine
`R.home()` is `<prefix>/lib/R` while the executable R actually runs is
`<prefix>/bin/exec/R`, a sibling of `R_HOME` that no other entry covers.
Granting a prefix is narrow where a tool owns its own directory (a Nix store
path, a Homebrew Cellar entry) and far too broad where the prefix is `/usr`, so
the resolver drops results matching the `pkg:` system-root denylist rather than
failing — on a distribution install the prefix is both dangerous and
unnecessary.

Grant `r:home` and `r:prefix` as **rox**, not `ro`: R's startup path runs
executables from both.

Constraints, carried over from the review of the portability plan:

- Resolvers are a fixed set implemented in Go against known interpreters. A
  profile can never name an arbitrary command to run.
- Every resolved path goes back through `paths.resolve()` so the symlink
  alias/target pairing and `refuseSensitiveSymlinkTarget` checks apply.
- Resolvers run before the sandbox starts, against the parent environment. The
  resulting paths must appear in `--policy=resolve` output, since a resolver
  that silently returns nothing is otherwise indistinguishable from a working
  one.
- A resolver failure (interpreter absent, non-zero exit) is not fatal; it
  resolves to nothing and is reported as skipped.
- Cap and deterministically sort resolver output so the policy is stable across
  runs.

## 9. Runtime library discovery

The `dlopen` problem from §3 needs an explicit decision. Options:

1. **Grant library trees, not individual libraries.** Whatever
   `.libPaths()` returns is granted read-execute rather than read-only, so
   package `.so` files can be loaded. Does not solve their *external*
   dependencies (`liblapack`, `libgfortran`), which live elsewhere.
2. **Walk installed packages' `libs/` directories and run ELF discovery on
   each `.so`.** Accurate, and reuses `elfdeps`. Cost is proportional to
   installed package count — 70 libraries here, potentially thousands of
   `.so` files on a full installation. Needs measurement before adopting.
3. **Grant the platform library roots from `defaults.toml`.** Works on Debian
   and Homebrew, fails on Nix where each library is its own store path.

Recommendation: option 1 as the baseline, option 2 behind `add_libs` with a
measured time budget, and a denial-diagnostics-driven fallback for the rest —
this is exactly the case the run-time hints were built for. Resolve before
implementation.

**This is now the blocker, confirmed by trying it.** With the resolvers in
place, an `r` profile resolves every R-owned path correctly and R still does not
start on this machine: the interpreter chain reaches `<prefix>/bin/exec/R`,
which needs `libgomp`, `libblas`, `liblapack`, `libreadline`, `libgcc_s`, and
more, each in a separate store path. `add_libs` does not find them because ELF
discovery runs against the wrapper, not the binary several `exec` hops down.
Following the denial hints one at a time works but does not converge quickly.
Whatever §9 decides must handle the *interpreter's own* runtime libraries, not
only those of installed packages.

## 10. Verification

Because these profiles make claims about third-party layout, they ship only if
CI can check them. Per profile, per platform:

```
bulle --profile r          -- Rscript -e 'stopifnot(nzchar(R.home())); cat("ok\n")'
bulle --profile r          -- Rscript -e 'library(stats); cat(length(.libPaths()), "\n")'
bulle --profile r-install  -- Rscript -e 'install.packages("brew", lib=Sys.getenv("R_LIBS_USER"))'
bulle --profile uv         -- uv run python -c 'import sys; print(sys.version)'
bulle --profile uv-install -- uv pip install --dry-run requests
```

Matrix: Ubuntu (apt R, `~/.local/bin/uv`), macOS (CRAN R, Homebrew uv), and one
Nix job — Nix is the adversarial case, and a profile that survives it will
survive the others. Record the versions each profile was verified against and
surface them in `--list-profiles --long`.

## 11. Open questions

1. Should `r` grant `.libPaths()` read-execute (needed to load compiled
   packages) or read-only (safe for pure-R packages only)? §9 option 1 assumes
   read-execute; confirm against a package with a `libs/` directory.
2. Does a bare `r` profile without `tool` need its own `TMPDIR` handling, or
   should it always inherit `tool`? With `TMPDIR`, `TMP`, and `TEMP` all unset,
   R writes to `/tmp` directly and fails.
3. Is `python` (bare interpreter, no uv) worth shipping given no system
   `python3` exists on this machine and layouts vary widely?
4. Should `-install` variants be separate profiles or a shared `--writable-libs`
   flag applying to any interpreter profile?
