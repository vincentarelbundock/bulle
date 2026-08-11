# SPEC: `--scratch` disposable workspaces

Status: draft. Scope: run a sandboxed command against a throwaway copy of the
workspace, then review and selectively keep the result.

## 1. Goal

```
bulle --scratch --profile claude
# ... agent works ...
# scratch: 4 files changed, 1 added
# [d]iff  [a]pply  [k]eep  [D]iscard?
```

The sandbox already limits *where* a command can write. `--scratch` limits
whether those writes reach the real checkout at all, turning "let the agent
try something" into a reviewable diff. It is the filesystem counterpart to
ephemeral state, which does the same for app config and caches.

## 2. Non-goals

- Snapshotting anything outside the workspace.
- Multi-workspace scratch (`--workspace` repeated) in v1.
- Merge conflict resolution. Apply either succeeds cleanly or reports a
  conflict and leaves the scratch in place.
- Automatic commits in the scratch or the origin.
- Parallel-session and branch-lifecycle management — that is `wt`-territory (§3).

## 3. Prior art: `wt`-style worktree managers, and why scratch still exists

A family of CLI tools (Worktrunk's `wt`, timvw's `wt`, `wtp`, `wtman`, …)
already automates "run an agent in a separate directory." All of them are
wrappers around `git worktree add`: templated paths, shell integration to `cd`
in, post-create hooks to copy `.env` and install dependencies, and a merge
command (`wt merge`) that commits, squashes, rebases, and fast-forwards the
result into the target branch. They are good at what they do, and a user who
trusts the agent can compose them with bulle today: `wt switch -c feature-x`
then `bulle --profile claude` inside the worktree. Nothing in bulle needs to
change to support that.

So the honest question is whether `--scratch` is worth building at all. It is,
because the two solve different problems:

- **Isolation model.** Every worktree shares the origin's `.git` — objects,
  refs, config, and hooks (§4 table). A sandboxed agent in a `wt` worktree has
  read-write reach into `.git/hooks`, where a written hook executes on the
  user's next commit outside the sandbox. The `wt` tools are convenience for a
  *trusted* context; scratch is containment for an *untrusted* one. Only
  `git clone --local` severs the pointer.
- **Integration workflow.** `wt merge` is a trusted-merge workflow: it commits
  and moves branches. Scratch's contract is the opposite — a review gate
  (§8) where nothing reaches the origin until the user has seen the diff, and
  apply never commits, stages, or touches branches.
- **Policy integration.** Retargeting `$WORKSPACE` and the automatic
  read-write grant, rewriting denial hints back to origin-relative paths, and
  `--policy` explaining the redirection can only live inside bulle.

Consequences for this spec:

- **Worktree mode is dropped.** An opt-in `--scratch=worktree` would be a
  worse `wt` (no path templates, no hooks, no merge) that also grants the
  origin `.git`, defeating scratch's one purpose. Users who want worktree
  ergonomics should use a `wt` tool around bulle; `--scratch` then carries a
  single meaning: isolated copy plus review gate. This resolves former open
  question 4.
- **Scope stays minimal.** Anything the `wt` tools already do well —
  parallel-session management, branch lifecycle, merge-to-main — is out of
  scope, permanently, not just for v1. The docs should name the composition
  explicitly ("for parallel trusted sessions, use a worktree manager; for
  reviewable untrusted runs, use `--scratch`").

## 4. Verified git behavior

Measured, not assumed. Setup: a repo with a commit, an executable
`.git/hooks/pre-commit`, one dirty tracked file, one untracked file.

| Behavior | `git clone --local` | `git worktree add` |
| --- | --- | --- |
| Hooks carried over | **No** — only `.git/hooks/*.sample` | **Yes** — `.git/hooks` is the origin's, shared |
| `.git` is self-contained | **Yes** — a real directory | **No** — a file reading `gitdir: <origin>/.git/worktrees/<name>` |
| Needs write access to origin `.git` | No | **Yes** — records state in `<origin>/.git/worktrees/<name>` |
| Objects | Hardlinked (link count 2), copy-on-write in practice since git never rewrites object files | Shared directly |
| Dirty tracked changes carried | No | No |
| Untracked files carried | No | No |
| Cost on a large repo | Cheap (hardlinks, no object copy) | Cheapest |

**This table decides the design.** A worktree's `.git` is a pointer into the
origin repository, so a worktree scratch cannot run without granting the origin's
`.git` read-write — which re-exposes exactly what scratch exists to protect,
including `.git/hooks`, where a written hook executes on the user's next commit
outside the sandbox. `git clone --local` is self-contained, drops hooks, and
costs almost nothing because objects are hardlinked.

Therefore: **clone is the default; worktree is not offered** (§3).

## 5. Modes

```
--scratch                 auto: clone if a git repo, copy otherwise
--scratch=clone           git clone --local
--scratch=copy            filesystem copy, reflink where available
--scratch-keep            skip the review prompt, keep the scratch, print its path
--scratch-dirty=<mode>    none | tracked | all   (default: tracked)
```

There is no worktree mode; see §3. If a user passes `--scratch=worktree`, error
with a pointer to `wt`-style tools for trusted parallel sessions.

### 5.1 `clone` (default for git repositories)

1. `git clone --local --no-checkout <origin> <scratch>`, then check out the
   origin's current `HEAD` (detached if the origin is detached).
2. Carry dirty state per `--scratch-dirty` (§6).
3. Workspace becomes `<scratch>`. The origin is **not** granted.

Use `--no-hardlinks` when the user passes `--scratch=copy` semantics explicitly;
hardlinked objects are safe under git's write patterns but mean the scratch is
not byte-independent from the origin.

### 5.2 `copy` (non-git workspaces)

1. Copy with reflinks where the filesystem supports them (`cp --reflink=auto`
   on Linux btrfs/XFS, `clonefile` on APFS), falling back to a plain copy.
2. Apply exclusions (§7) before copying, not after.
3. Refuse and report if the estimated size exceeds a threshold
   (`--scratch-max-size`, default 2 GiB) unless the user overrides.

## 6. Dirty state

A fresh clone is at `HEAD`, so uncommitted work is absent by
default — the single most surprising thing about a naive implementation, since
the agent silently starts from a state the user is not looking at.

`--scratch-dirty` controls this:

- `tracked` (default): apply `git diff HEAD` from the origin into the scratch.
- `all`: also copy untracked, non-ignored files (`git ls-files --others
  --exclude-standard`).
- `none`: start from a clean `HEAD`.

Report what was carried: `scratch: from HEAD + 3 modified, 1 untracked`.

If the diff fails to apply (submodules, mode changes, binary files without
suitable context) fail loudly before running the command rather than proceeding
from a state neither the user nor the agent expects.

## 7. Location and exclusions

Scratches live outside both the origin and the temporary directory:

```
$XDG_STATE_HOME/bulle/scratch/<workspace-basename>-<8-char-id>/
```

Not inside the origin: a scratch under the repository would be picked up by
`git status`, editor indexers, and build tools. Not in `$TMP`: scratches must
survive until the user reviews them, and `/tmp` cleaners are not that patient.

Copy mode excludes by default: `.git` (handled separately), and any path
matching `node_modules/`, `.venv/`, `venv/`, `target/`, `.tox/`, `__pycache__/`,
`.mypy_cache/`, `dist/`, `build/`. Configurable through `--scratch-exclude` and
a `[scratch] exclude` list in the user config. Exclusions are reported, since a
silently omitted `node_modules` produces a confusing failure inside the sandbox.

## 8. Review

After the command exits, compute the change set:

- git modes: `git -C <scratch> status --porcelain` plus a diff against the
  recorded base.
- copy mode: recursive comparison against the origin.

If the change set is empty, remove the scratch silently and exit — a run that
changed nothing should leave nothing behind.

Otherwise, when stdin is a TTY and `--scratch-keep` was not passed:

```
scratch: 4 files changed, 1 added, 0 deleted
  M internal/policy/resolve.go
  M internal/policy/policy.go
  A internal/policy/scratch.go
  ...
[d]iff  [a]pply  [k]eep  [D]iscard?
```

- `d` — page the full diff, then re-prompt.
- `a` — apply to the origin (§8.1), then offer to discard the scratch.
- `k` — keep and print the path.
- `D` — delete after a confirmation naming the file count.

Non-interactive runs (no TTY, or `--scratch-keep`) print the summary and the
path, and keep the scratch. **A scratch is never deleted implicitly**, including
on timeout, signal, or crash: losing an agent's work to a cleanup path is worse
than leaving a directory behind, and §9 provides the cleanup route.

### 8.1 Apply

Refuse to apply when the origin's working tree has changed since the scratch was
created (compare recorded `HEAD` and a dirty-state hash). Otherwise generate a
patch and `git apply --3way` in git modes, or copy changed files in copy mode.
On conflict, report the conflicting paths, apply nothing, and keep the scratch.

Apply never commits, never stages, and never touches branches in the origin.

## 9. Lifecycle commands

```
bulle scratch list          # id, origin, age, change count, size
bulle scratch path <id>
bulle scratch diff <id>
bulle scratch apply <id>
bulle scratch rm <id>
bulle scratch gc            # remove scratches with no changes, or older than N days
```

Metadata lives beside each scratch in `meta.toml`: origin path, mode, base
commit, dirty-state hash, creation time, and the bulle invocation.

## 10. Integration points

- **`cli.Flags`**: add `Scratch string`, `ScratchKeep bool`, `ScratchDirty string`,
  `ScratchExclude []string`, `ScratchMaxSize string`. `--scratch` with no value
  normalizes to `auto` the way `--policy` normalizes to `summary`
  (`normalizePolicyFormat` in `internal/cli/parse.go` is the pattern).
- **`app.Run`**: create the scratch after defaults are applied and the command
  is known, but **before** `policy.Resolve`, then set `opts.ProjectPath` to the
  scratch path so `$WORKSPACE` and the automatic read-write grant follow it with
  no changes in `internal/policy`.
- **Cleanup**: unlike shim directories, scratches are *not* removed by the
  deferred cleanup in `app.Run`. Only an empty change set (§7) or an explicit
  discard removes one.
- **`--policy`**: the resolution table shows the scratch path as the workspace
  and names its origin, so `--scratch --policy` explains itself without running
  anything.
- **`--last`**: replays as a *new* scratch rather than reusing the previous one.
- **Denial diagnostics**: hints must report paths relative to the scratch, and
  rewrite scratch paths back to origin-relative form so a suggested `--ro` grant
  is meaningful on the next run.

## 11. Failure modes

| Situation | Behavior |
| --- | --- |
| Not a git repo, `--scratch=clone` | Error naming `copy` as the alternative |
| Repo has no commits | Fall back to `copy` with a note |
| Origin has submodules | v1: warn that submodules are not carried |
| Scratch creation fails midway | Remove the partial scratch, exit `ExitConfigError` |
| Command times out | Scratch kept; summary still printed |
| Disk full during copy | Remove the partial scratch, report the shortfall |
| `--scratch` with `--no-workspace` | Error: contradictory |
| Origin inside an existing scratch | Refuse to nest |

## 12. Open questions

1. Should `--scratch` imply ephemeral state? An agent writing its own config in
   a scratch run arguably should not persist it either, but coupling the two
   removes the ability to choose.
2. Is `apply` worth building in v1, or is `keep` plus the printed path enough?
   Apply is the feature people will ask for and also the only part that writes
   to the origin.
3. Should copy mode honor `.gitignore` in non-git directories that happen to
   have one, or only the built-in exclusion list?

Resolved: worktree mode is dropped entirely (§3), so `--scratch` carries a
single meaning — isolated copy plus review gate.
