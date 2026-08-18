#import "/.calepin/calepin.typ" as calepin

#set document(title: [Profiles])
#metadata((
  title: "Profiles",
  description: "Named bundles of permissions: explicit selection, the TOML format, portable path entries, and how to write your own.",
)) <website-metadata>

#title()

A profile is a named bundle of path, environment, network, and platform grants. It saves you from spelling out the same permissions every time you run a tool.

#calepin.elements.callout(kind: "warning", title: none)[
  Profiles can grant broad filesystem, environment, network, and platform access. Use `bulle show PROFILE` to inspect the resolved permissions before running an unfamiliar profile or combining profiles.
]

= Profile selection

Profiles are applied only by explicit selection. The first positional argument names the profile. When it declares a `default_app`, you do not need to pass a command --- this launches Claude Code with appropriate permissions and constraints:

```bash
bulle claude
```

`bulle` never infers a profile from an executable name. A repository-controlled
file called `codex` or `claude` must not acquire that agent's credentials,
network access, or writable state merely because its basename matches a
`default_app`. Use `bulle show NAME` to inspect a profile before
selecting it.

An explicit command after `--` gets only its own executable and runtime libraries automatically. It does not select a profile or inherit that tool's credentials, state directories, or network policy:

```text
bulle -- /usr/bin/printf 'minimal sandbox\n'
```

The built-in `tool` profile adds `PATH`, executable discovery, temporary directory access, runtime library access, and network access:

```text
bulle tool -- ping google.com

PING google.com (...): 56 data bytes
64 bytes from ...
```

Comma-separated profiles merge from left to right. Adding `offline` after `tool` keeps the command setup but removes network access:

```text
bulle tool,offline -- ping google.com

ping: cannot resolve google.com: Unknown host
```

You can still add one-off permissions on top of a profile:

```text
bulle claude --ro README.qmd --rw ~/Desktop --env GITHUB_TOKEN
```

= Built-in and installed profiles

Use `bulle show profiles` to print available profiles:

```bash
bulle show profiles

claude
codex
coreutils
default
git
go
gui
keychain
latex
macos-certs
macos-devtools
macos-dns
network
node
offline
opencode
pandoc
pi
quarto
r
r-install
rust
terminal
tool
user-bin
uv
uv-install
```

Built-in helper profiles such as `default`, `network`, `offline`, `macos-dns`, `macos-certs`, `macos-devtools`, and `keychain` are ordinary profiles that can be inherited directly or selected explicitly when you pass a command.

== Capability micro-profiles

The built-in `git`, `node`, `rust`, `go`, and `terminal` profiles are small capability bundles, each answering one concern once and portably. The tool profiles use `which:`/`pkg:` resolvers, tool resolvers such as `npm:cache` and `go:modcache`, platform variables, and optional/create markers to settle a tool's location questions (binary, configuration, caches); `terminal` carries the environment variables an interactive program expects. They are meant to be assembled through `inherits` rather than restating tool layouts in every agent profile:

```toml
title = "My agent"
inherits = ["tool", "git", "node"]
default_app = "my-agent"
```

They can also be combined on the command line: `bulle tool,git,rust -- cargo build`.

== Installing profiles

Install or override profiles with `bulle profiles install SOURCE`. The source can be one `.toml` file, a directory containing `.toml` files, a local git repository, or a GitHub source such as `github:vincentarelbundock/bulle/custom_profiles`.

```bash
bulle profiles install agent.toml
bulle profiles install ./profiles
bulle profiles install github:vincentarelbundock/bulle/custom_profiles
```

By default, profiles are installed under the operating system user config directory: usually `$XDG_CONFIG_HOME/bulle/profiles/` or `~/.config/bulle/profiles/` on Linux, and `~/Library/Application Support/bulle/profiles/` on macOS. Use `--config PATH` to install into a different config directory; `bulle` creates its `profiles/` subdirectory if needed. The filename becomes the profile name, so `profiles/agent.toml` is selected with `bulle agent`.

Installing prints the grants each file declares, because an installed profile decides what a sandbox permits. A file named after a profile you already have is not replaced unless you pass `--force`: the merge also reaches every profile that inherits from that name, so an innocuous-looking `node.toml` can widen runs that never mention `node`.

When installing from a local git repository root or `github:owner/repo`, `bulle` uses `profiles/*.toml` if that directory exists. When a GitHub source includes a subdirectory, such as `github:owner/repo/custom_profiles`, that subdirectory is used as the profile source.

= The TOML format
<toml>

Built-in and user profiles use the same one-profile TOML format. The filename is the profile name, and profile fields live at the top level of that file. This example shows all profile option groups:

```toml
title = "Agent"
description = "custom Codex profile"
inherits = ["tool", "keychain"]
default_app = "codex"

ro = ["README.md"]
rox = ["/usr/bin"]
rw = ["$TMP/bulle/tmp"]
rwx = ["$HOME/.cache/example-agent"]

env = ["HOME", "USER", "NODE_ENV=development"]
allow = ["network"]
deny = []

add_exec = true
add_libs = true

[macos]
ro = ["$HOME/Library/Preferences"]
mach_lookup = ["com.apple.trustd.agent"]
deny_mach_lookup = ["com.apple.SystemConfiguration.configd"]

[linux]
ro = ["$HOME/.config"]
rox = ["/usr/bin"]
```

Available top-level options are `title`, `description`, `inherits`, `default_app`, path grants (`ro`, `rox`, `rw`, `rwx`), `env`, network settings (`allow`, `deny`), macOS Mach services (`mach_lookup`, `deny_mach_lookup`), executable discovery defaults (`add_exec`, `add_libs`), and platform tables (`[macos]`, `[linux]`). Only `title` and `description` are metadata fields.

Path grants can use variables and entry markers; see #link("#portable-profiles")[Portable profiles] below.

`inherits` can be one profile name or an array of profile names. Parents are merged left to right and the child is applied last. Path grants merge by path and promote permissions, so `rox` plus `rw` for the same path becomes `rwx`. Environment entries merge by variable name with later values winning. Network and Mach allow/deny entries supersede by name.

`env` entries can be variable names copied from the parent environment or explicit `KEY=value` assignments. The only current network capability name is `network`, so `allow = ["network"]` enables network access and `deny = ["network"]` disables it.

The `[macos]` and `[linux]` tables are applied only on that platform. They accept `default_app`, path grants, `env`, `allow`, `deny`, `mach_lookup`, `deny_mach_lookup`, `add_exec`, and `add_libs`. They do not accept profile metadata or `inherits`.

= Portable profiles
<portable-profiles>

A profile written on one machine should work on another, even when tools are installed in different places. Several features make path entries portable.

== Path variables

`$WORKSPACE` is the workspace path; `$HOME`, `$TMP`, and `$TMPDIR` are fixed. Four variables resolve per platform, so one entry covers both operating systems:

#table(
  columns: 3,
  table.header([Variable], [Linux], [macOS]),
  [`$CONFIG`], [`$XDG_CONFIG_HOME` or `~/.config`], [`~/Library/Application Support`],
  [`$DATA`], [`$XDG_DATA_HOME` or `~/.local/share`], [`~/Library/Application Support`],
  [`$CACHE`], [`$XDG_CACHE_HOME` or `~/.cache`], [`~/Library/Caches`],
  [`$STATE`], [`$XDG_STATE_HOME` or `~/.local/state`], [`~/Library/Application Support`],
)

On Linux, the raw `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, and `XDG_STATE_HOME` names are also available and honor the parent environment when set. On macOS, several of these collapse to the same directory; the `bulle show` resolution table flags entries whose grants merge because of that.

== Optional and created entries

Read-only entries (`ro`, `rox`) are always skipped silently when the path does not exist. Writable entries (`rw`, `rwx`) fail hard on a missing path unless marked:

```toml
rw  = ["?$HOME/.netrc"]        # ? optional: skip when missing
rwx = ["+$HOME/.codex/"]       # + trailing slash: create the directory when missing
rw  = ["+$HOME/.claude.json"]  # + no slash: create an empty file when missing
```

Creation is the right default for a tool's own state: silently skipping a missing writable grant would produce a mysterious permission error later, and the built-in agent profiles use `+` for their state directories so they work on a fresh machine.

== Executable resolvers

`which:NAME` looks up `NAME` on the parent `PATH` at policy-build time and grants exactly that binary (and its symlink chain) --- never its containing directory, so `which:codex` does not hand over everything else in the same `bin`. `pkg:NAME` additionally grants the tool's package tree (two levels above the real binary), which Node- and Python-based tools need for their libraries; it refuses to expand when that tree would be a system directory such as `/usr`. Both are valid only in `rox` and `rwx`:

```toml
rox = ["which:git", "which:rg", "pkg:codex"]
```

Name lookup inside the sandbox works through a per-run shim directory of symlinks that `bulle` creates outside the sandbox's writable area, grants read+execute, prepends to `PATH`, and removes after the run. A `which:`-based profile must name every binary the tool shells out to; missing ones surface through #link("denial-diagnostics.html")[denial diagnostics]. The broad-grant `tool` profile remains the escape hatch.

== Tool resolvers

Some layouts cannot be written down at all. R's package search path holds one entry per installed package --- 70 of them on a Nix machine, each a separate store path, changing on every update. Instead of guessing, ask the tool: an entry of the form `TOOL:ASPECT` runs a known query before the sandbox starts and grants whatever it reports.

```toml
rox = ["r:home", "r:prefix"]
ro  = ["r:libs"]
rw  = ["?+npm:cache", "?+go:modcache"]
```

Unlike `which:`/`pkg:`, these name ordinary directories and are valid in every list. Markers work as usual: `?` skips when the tool is absent, `+` creates a directory the tool reports but has not made yet. Results are re-resolved as literal paths, so the symlink pairing and sensitive-target refusals apply exactly as they do to a hand-written path.

Run `bulle resolvers` to see every resolver and what it points at on the current machine --- a resolver that reports nothing is otherwise indistinguishable from one that works. The set is fixed by `bulle`: a profile chooses from it but can never supply a command to run. Before executing a resolver, `bulle` resolves its real executable and refuses it when the file or any enclosing directory is writable by the current user. Resolver processes receive a narrow, tool-specific environment with credentials and unrelated variables removed. This intentionally rejects project virtualenvs and user-owned tool installations: code that has not entered the sandbox cannot be trusted merely because its filename is `go` or `Rscript`. An unknown namespace is an error rather than a literal path, and a literal path in that shape must be written `./ruby:gems` or absolute.

Some aspects are guarded. `r:prefix` reports R's installation root, which is narrow where R owns its own directory (a Nix store path, a Homebrew Cellar entry) and far too broad where it is `/usr`; results matching the `pkg:` system-root denylist are dropped rather than granted.

== Globs

A `*` matches within one path segment (no `**`). No matches means the entry is skipped, so version-stamped directories stop breaking on upgrade:

```toml
rox = ["$HOME/.nvm/versions/node/*/bin"]
```

== Custom variables

Machine-specific layouts live in a `[vars]` table in `<config>/config.toml` (usually `~/.config/bulle/config.toml`), or one-off with `--var NAME=VALUE`:

```toml
[vars]
PROJECTS = "/mnt/work/repos"
```

Profiles then reference `$PROJECTS`, and `${NAME:-fallback}` supplies a default when a variable is unset. A small allowlist of well-known tool environment variables (`CARGO_HOME`, `GOPATH`, `NVM_DIR`, `PYENV_ROOT`, and similar) may also be referenced; their values come from the parent environment and are treated as untrusted --- values that are not absolute paths, or that resolve to `/` or the home directory, are ignored so a hostile environment cannot widen a grant. Custom variable names are uppercase, and reserved names (`HOME`, `WORKSPACE`, `TMP`, `CONFIG`, `DATA`, `CACHE`, `STATE`, `XDG_*`, ...) cannot be redefined.

= Writing a tool profile
<writing-profiles>

This section is the recipe for adding a language or tool profile to bulle. It distills what shipping the `r` and `uv` profiles taught: comprehensiveness comes from asking the tool where its files are instead of hardcoding paths, and constraint comes from an offline, read-only base profile with an opt-in `-install` variant.

== The slot checklist

Every language toolchain decomposes into the same handful of grants. Fill in each slot, or note why it does not apply:

#table(
  columns: 3,
  table.header([Slot], [Typical list], [Example]),
  [Interpreter / launcher binaries], [`rox`], [`?which:R`, `?which:uv`],
  [Tool home (stdlib, `etc/`, launch scripts)], [`rox`], [`?r:home`, `?go:root`],
  [Library search path], [`ro`], [`?r:libs`],
  [User-writable library], [`rw` in `-install` only], [`?+r:libs-user`],
  [Package cache], [`ro`, or redirected], [`?uv:cache`, `UV_NO_CACHE=1`],
  [Runtime cache the tool insists on writing], [redirect into sandbox tmp], [`DENO_DIR=$TMP/bulle/tmp/deno`],
  [Config and startup files], [`ro`, optional], [`?~/.Rprofile`, `?$CONFIG/uv`],
  [Temp], [inherited from `tool`], [---],
  [Environment variables], [`env`], [`R_LIBS_USER`, `UV_CACHE_DIR`],
  [Network], [`deny` in base, `allow` in `-install`], [---],
)

== House rules

+ *Ask the tool, don't guess.* Interpreter layout is not predictable (Nix
  store paths, Homebrew kegs, version managers). Use `which:NAME` for
  binaries and a tool resolver (`r:home`, `uv:cache`, `npm:cache`) for
  directories. Literal paths are fallbacks for when the tool is absent.
  `bulle resolvers` shows every resolver and what it reports on the
  current machine.
+ *Base profiles are offline and read-only for libraries.* A sandboxed
  process that can write the user library or package cache can plant code
  that the user's own non-sandboxed sessions will load later. That turns
  "wrote in the workspace" into "wrote code that runs outside the sandbox."
  Installation is opt-in through a separate `<name>-install` profile that
  inherits the base, adds the writable library or cache, and allows network.
+ *Mark machine-dependent entries optional (`?`).* A resolver that finds
  nothing, or a startup file that does not exist, must not break the run.
  Never force an environment variable's value: list the name so an
  explicitly configured value passes through, and let the tool compute its
  default otherwise. When a tool insists on a writable cache even for
  offline runs, do not grant its real cache: set an explicit value that
  redirects it into the sandbox tmp --- profile `env` values expand path
  variables, so `"DENO_DIR=$TMP/bulle/tmp/deno"` works on every platform
  (`UV_NO_CACHE=1` and quarto's `DENO_DIR` are the shipped examples).
+ *Do not build shared platform abstractions.* uv follows XDG on macOS;
  R uses `~/Library` there. Each profile encodes (or queries) its own
  tool's convention.
+ *Let the engine handle runtime libraries.* `add_libs = true` makes bulle
  follow wrapper scripts and shebang chains inside the granted trees, find
  every ELF object (including package `libs/` directories on the read-only
  search path), resolve their `DT_NEEDED`/`RPATH` closures, and grant the
  results. Do not enumerate `libblas`/`libgfortran`-style dependencies in
  profile text.
+ *No profile ships without smoke tests.* Add a row to the profile smoke
  table in `internal/integration/profile_smoke_test.go`. The CI matrix runs
  it on Ubuntu, macOS, and Nix; Nix is the adversarial case, and a profile
  that survives it survives the others.

== Skeleton

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

== Adding a resolver

Resolvers are a fixed registry in `internal/policy/tools.go` --- profiles can
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

Constraints the registry enforces for you: resolvers use an immutable
executable and a scrubbed, tool-specific environment before the sandbox
starts; results re-enter normal path
resolution (symlink pairing, sensitive-target refusals), failures are
non-fatal for `?` entries, and output is deduplicated and sorted so the
policy is stable across runs. Every result is also passed through the system
root and sensitive-path checks, so `/usr` or a home-directory root never
becomes a grant.

== Debugging a new profile

Run the target command under the profile and read the denial report: bulle
aggregates denials inside one Nix store item or Homebrew keg into a single
suggested grant and prints the flags to add to the next invocation.
`bulle show PROFILE -- command` shows the fully resolved grants; add `--json`
for
machine-readable. Recurring denials usually mean a missing slot from the
checklist above rather than a one-off path.
