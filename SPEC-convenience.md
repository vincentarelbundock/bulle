# SPEC: Convenience improvements

Proposals to improve the day-to-day convenience of bulle, from a codebase
survey (2026-08-15). Ordered by payoff per effort. Completed items are
removed as they land (shell completions and per-subcommand help shipped, with
the flag/subcommand tables that drive them shared for reuse — see "Shared
plumbing").

## High payoff

### 2. Per-project config (`.bulle.toml`)

The common invocation `bulle --profile claude ~/repos/project` is retyped
constantly. A small `.bulle.toml` at the workspace root (profile, extra
grants, limits) collapses it to `bulle`, composing with the existing
`[defaults]` merge in `internal/config/config.go`.

Trust caveat: a repo-supplied file choosing sandbox policy is a trust
inversion. Either require the file to be listed/approved in user config, or
honor only grant-*narrowing* keys plus the profile name, with a first-use
confirmation.

### 3. `bulle profiles show NAME` and richer `profiles list`

- `profiles list` prints bare names today (`internal/app/profiles_cmd.go:34-36`)
  even though descriptions exist and `--help` already formats a two-column
  view (`usage.go:110-115`). Add descriptions and origin (built-in vs
  `~/.config/bulle/profiles/`).
- Add `profiles show NAME`: print a profile's resolved contents — inheritance
  flattened, variables noted — so users can answer "what would this grant?"
  without running `policy` against a command.
- Add `profiles validate FILE` for authoring; today validation happens only
  implicitly at run time, and only for the selected profile.

## Medium payoff

### 6. Friendlier errors

- Misspelled profile names get no suggestion despite `ProfileNames` being
  available — add a did-you-mean.
- Every flag-parse failure is wrapped as `"%s (run 'bulle --help')"`
  (`internal/cli/parse.go:172-174`); point at the specific flag's syntax
  instead.
- `rerun: no previous invocation recorded` (`app.go:93`) should say what
  creates the record and where it lives.
- `loadConfig` silently ignores a missing `config.toml` (`app.go:401-405`),
  so a typo'd directory is invisible. Add `bulle config` (or `policy
  --config-path`) printing which config files were loaded and where one would
  be created.

### 7. `record --install NAME`

Recording is the flow that writes files, but `--out` makes the user pick a
path and separately install it. Let record write a validated profile straight
into `~/.config/bulle/profiles/NAME.toml` and print the `bulle --profile
NAME` invocation to use next.

## Lower priority

### 8. A real README

The shipped README is 8 lines. Release-archive and GitHub-first users need
install + one quickstart example + a pointer to `record`, even with the
website as canonical docs.

### 9. Distribution breadth

- Mention `go install` in the docs' install section.
- Consider an nfpm block for deb/rpm, and a Nix flake.
- Default `install.sh` to `~/.local/bin` when not root, instead of the
  sudo-requiring `/usr/local/bin`.

### 10. Man page

Generate roff from the same source as the help text (or the Typst CLI
reference) and ship it in archives and the brew formula.

## Shared plumbing

The completion work left behind two drift-proof tables: flag specs derived by
reflection from the `Flags` struct (`internal/cli/flagspec.go`) and the
subcommand table shared with dispatch (`internal/app/commands.go`), plus
per-subcommand help topics (`internal/cli/help.go`, sync-tested against the
table). Item 6 (per-flag error messages) should build on those rather than
adding parallel listings.
