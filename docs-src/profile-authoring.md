# Writing profiles

This page is the recipe for adding a language or tool profile to bulle. It
distills what shipping the `r` and `uv` profiles taught: comprehensiveness
comes from asking the tool where its files are instead of hardcoding paths,
and constraint comes from an offline, read-only base profile with an opt-in
`-install` variant.

## The slot checklist

Every language toolchain decomposes into the same handful of grants. Fill in
each slot, or note why it does not apply:

| Slot | Typical list | Example |
| --- | --- | --- |
| Interpreter / launcher binaries | `rox` | `?which:R`, `?which:uv` |
| Tool home (stdlib, `etc/`, launch scripts) | `rox` | `?r:home`, `?go:root` |
| Library search path | `ro` | `?r:libs` |
| User-writable library | `rw` in `-install` only | `?+r:libs-user` |
| Package cache | `ro`, or redirected | `?uv:cache`, `UV_NO_CACHE=1` |
| Runtime cache the tool insists on writing | redirect into sandbox tmp | `DENO_DIR=$TMP/bulle/tmp/deno` |
| Config and startup files | `ro`, optional | `?~/.Rprofile`, `?$CONFIG/uv` |
| Temp | inherited from `tool` | — |
| Environment variables | `env` | `R_LIBS_USER`, `UV_CACHE_DIR` |
| Network | `deny` in base, `allow` in `-install` | — |

## House rules

1. **Ask the tool, don't guess.** Interpreter layout is not predictable (Nix
   store paths, Homebrew kegs, version managers). Use `which:NAME` for
   binaries and a tool resolver (`r:home`, `uv:cache`, `npm:cache`) for
   directories. Literal paths are fallbacks for when the tool is absent.
   `bulle --list-resolvers` shows every resolver and what it reports on the
   current machine.
2. **Base profiles are offline and read-only for libraries.** A sandboxed
   process that can write the user library or package cache can plant code
   that the user's own non-sandboxed sessions will load later. That turns
   "wrote in the workspace" into "wrote code that runs outside the sandbox."
   Installation is opt-in through a separate `<name>-install` profile that
   inherits the base, adds the writable library or cache, and allows network.
3. **Mark machine-dependent entries optional (`?`).** A resolver that finds
   nothing, or a startup file that does not exist, must not break the run.
   Never force an environment variable's value: list the name so an
   explicitly configured value passes through, and let the tool compute its
   default otherwise. When a tool insists on a writable cache even for
   offline runs, do not grant its real cache: set an explicit value that
   redirects it into the sandbox tmp — profile `env` values expand path
   variables, so `"DENO_DIR=$TMP/bulle/tmp/deno"` works on every platform
   (`UV_NO_CACHE=1` and quarto's `DENO_DIR` are the shipped examples).
4. **Do not build shared platform abstractions.** uv follows XDG on macOS;
   R uses `~/Library` there. Each profile encodes (or queries) its own
   tool's convention.
5. **Let the engine handle runtime libraries.** `add_libs = true` makes bulle
   follow wrapper scripts and shebang chains inside the granted trees, find
   every ELF object (including package `libs/` directories on the read-only
   search path), resolve their `DT_NEEDED`/`RPATH` closures, and grant the
   results. Do not enumerate `libblas`/`libgfortran`-style dependencies in
   profile text.
6. **No profile ships without smoke tests.** Add a row to the profile smoke
   table in `internal/integration/profile_smoke_test.go`. The CI matrix runs
   it on Ubuntu, macOS, and Nix; Nix is the adversarial case, and a profile
   that survives it survives the others.

## Skeleton

```toml
# profiles/mytool.toml
title = "MyTool"
description = "mytool interpreter and libraries"
inherits = ["tool", "terminal"]

rox = ["?which:mytool", "?mytool:home"]
ro = ["?mytool:libs", "?$CONFIG/mytool"]
env = ["MYTOOL_HOME", "MYTOOL_OPTIONS"]
deny = ["network"]
add_libs = true
```

```toml
# profiles/mytool-install.toml
title = "MyTool (with package installation)"
description = "mytool plus a writable user library and registry access"
inherits = "mytool"

rw = ["?+mytool:libs-user"]
allow = ["network"]
```

## Adding a resolver

Resolvers are a fixed registry in `internal/policy/tools.go` — profiles can
never name an arbitrary command to run, because installing a profile must not
be equivalent to running its author's code. Adding support for a new tool is
usually two to four table rows of the form:

```go
{
    tool: "mytool", aspect: "libs",
    argv:        []string{"mytool", "config", "libdir"},
    format:      formatSingle, // or formatLines
    description: "mytool's library directory",
},
```

Constraints the registry enforces for you: resolvers run against the parent
environment before the sandbox starts, results re-enter normal path
resolution (symlink pairing, sensitive-target refusals), failures are
non-fatal for `?` entries, and output is deduplicated and sorted so the
policy is stable across runs. Set `denySystemRoots: true` on any aspect that
can report an installation prefix, so `/usr` never becomes a grant.

## Debugging a new profile

Run the target command under the profile and read the denial report: bulle
aggregates denials inside one Nix store item or Homebrew keg into a single
suggested grant and prints a copy-pasteable `bulle --last …` retry line.
`--policy` shows the fully resolved grants; `--policy=json` is
machine-readable. Recurring denials usually mean a missing slot from the
checklist above rather than a one-off path.
