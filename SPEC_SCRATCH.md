# SPEC: `--scratch` disposable workspaces

Status: draft. Scope: run a sandboxed command against a throwaway clone of the
workspace, review the result, and integrate it with plain git.

## 1. Goal

```
bulle --scratch --profile claude
# ... agent works ...
# scratch: 4 files changed, 1 added
# [d]iff  [k]eep  [D]iscard?
```

The sandbox already limits *where* a command can write. `--scratch` limits
whether those writes reach the real checkout at all, turning "let the agent
try something" into a reviewable diff. Integration is git-native: when
satisfied, the user pushes from the scratch to the origin (§8.1).

The whole model in three sentences: `--scratch` clones the repository locally
(carrying uncommitted work), runs the sandboxed command there, and shows a
diff at the end. The user keeps or discards. Nothing reaches the origin except
a deliberate `git push` from outside the sandbox.

## 2. Non-goals

- Non-git workspaces. Scratch requires a git repository with at least one
  commit; `git init && git add -A && git commit` is the escape hatch, and it
  buys the review diff for free.
- Snapshotting anything outside the workspace.
- Multi-workspace scratch (`--workspace` repeated).
- A bespoke apply/merge step. Integration is `git push` plus normal git in the
  origin (§8.1); merge conflicts are git's to report.
- Automatic commits in the origin.
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
  and moves branches on the agent's output directly. Scratch's contract is the
  opposite — a review gate (§8) where nothing reaches the origin until the
  user has seen the diff and pushed it themselves, from outside the sandbox.
- **Policy integration.** Retargeting `$WORKSPACE` and the automatic
  read-write grant, rewriting denial hints back to origin-relative paths, and
  `--policy` explaining the redirection can only live inside bulle.

Consequences for this spec:

- **Worktree mode is dropped.** An opt-in `--scratch=worktree` would be a
  worse `wt` (no path templates, no hooks, no merge) that also grants the
  origin `.git`, defeating scratch's one purpose. Users who want worktree
  ergonomics should use a `wt` tool around bulle.
- **Scope stays minimal.** Anything the `wt` tools already do well —
  parallel-session management, branch lifecycle, merge-to-main — is out of
  scope, permanently. The docs should name the composition explicitly ("for
  parallel trusted sessions, use a worktree manager; for reviewable untrusted
  runs, use `--scratch`").

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
origin repository, so a worktree scratch cannot run without granting the
origin's `.git` read-write — which re-exposes exactly what scratch exists to
protect, including `.git/hooks`, where a written hook executes on the user's
next commit outside the sandbox. `git clone --local` is self-contained, drops
hooks, and costs almost nothing because objects are hardlinked.

Therefore: **scratch is a local clone; worktree is not offered** (§3).

## 5. Mechanism

```
--scratch        clone the workspace, run there, review after
--scratch-keep   skip the review prompt, keep the scratch, print its path
```

`--scratch` is a boolean; there are no modes. `--scratch=worktree` errors with
a pointer to `wt`-style tools for trusted parallel sessions.

Creation:

1. `git clone --local --no-checkout <origin> <scratch>`, then check out the
   origin's current `HEAD` (detached if the origin is detached).
2. Carry dirty state (§6).
3. Workspace becomes `<scratch>`. The origin path is **not** granted to the
   sandbox — during the run, nothing in the scratch can reach it, including
   `git push` (§8.1).

The `origin` remote is kept pointing at the origin's path. It is inert during
the run (the sandbox denies the origin) and is the integration route after it
(§8.1). Hardlinked objects are safe under git's write patterns — git never
rewrites object files in place — and are what makes cloning nearly free on
large repositories.

## 6. Dirty state

A fresh clone is at `HEAD`, so uncommitted work would be absent — the single
most surprising thing about a naive implementation, since the agent would
silently start from a state the user is not looking at.

Therefore the scratch always starts from what the user sees: after checkout,
apply the origin's `git diff --binary HEAD` and copy untracked, non-ignored
files (`git ls-files --others --exclude-standard`). There is no flag; a user
who wants a clean-`HEAD` run can commit or stash first — the git-native way to
say so.

Report what was carried: `scratch: from HEAD + 3 modified, 1 untracked`.

If the diff fails to apply (submodules, exotic mode changes), fail loudly
before running the command rather than proceeding from a state neither the
user nor the agent expects.

## 7. Location

Scratches live outside both the origin and the temporary directory:

```
$XDG_STATE_HOME/bulle/scratch/<workspace-basename>-<8-char-id>/
```

Not inside the origin: a scratch under the repository would be picked up by
`git status`, editor indexers, and build tools. Not in `$TMP`: scratches must
survive until the user reviews them, and `/tmp` cleaners are not that patient.

The location is configurable via `[scratch] dir` in the user config, for users
whose repos live on a different filesystem than `$XDG_STATE_HOME` (a sibling
directory of the origin is the natural alternative). When the scratch root and
the origin are on different filesystems (compare device IDs), hardlinking is
impossible and git silently falls back to copying the object store; detect
this and warn before cloning a large repository.

Scratches are created under a temporary name and renamed into place only when
creation fully succeeds, so a crash mid-creation can never leave something
that looks like a reviewable scratch but is a torn copy.

Metadata lives beside each scratch in `meta.toml`: origin path, base commit,
creation time, and the bulle invocation. Nothing in v1 reads it; it exists so
future tooling (a `list`/`gc` command, if demand materializes) needs no format
migration.

## 8. Review

After the command exits, compute the change set with
`git -C <scratch> status --porcelain` plus a diff against the recorded base:
`HEAD` as carried (§6), so the user's own uncommitted work does not show up in
the agent's diff. Concretely, record the post-carry state as a synthetic
baseline (e.g. a temporary index/tree object) at creation time.

If the change set is empty, remove the scratch silently and exit — a run that
changed nothing should leave nothing behind. "Empty" means no difference
against the recorded baseline; a clean `status --porcelain` alone never
triggers removal, because the agent may have *committed* its work in the
scratch, leaving a clean status over very real unreviewed changes.

Otherwise, when stdin is a TTY and `--scratch-keep` was not passed:

```
scratch: 4 files changed, 1 added, 0 deleted
  M internal/policy/resolve.go
  M internal/policy/policy.go
  A internal/policy/scratch.go
  ...
[d]iff  [k]eep  [D]iscard?
```

- `d` — page the full diff, then re-prompt.
- `k` — keep; print the path and the integration hint (§8.1).
- `D` — delete after a confirmation naming the file count.

Non-interactive runs (no TTY, or `--scratch-keep`) print the summary and the
path, and keep the scratch. **A scratch is never deleted implicitly**,
including on timeout, signal, or crash: losing an agent's work to a cleanup
path is worse than leaving a directory behind. The only removals are the
empty-change-set case above and an explicit `D`/`rm -rf`.

### 8.1 Integration: git push

There is no apply step. The scratch is a real repository with `origin`
pointing at the real one, so integration is ordinary git, done by the user,
outside the sandbox:

```
cd <scratch>
git diff                       # review again if desired
git add -A && git commit
git push origin HEAD:scratch/<id>
cd <origin>
git merge scratch/<id>         # or rebase, cherry-pick, diff first, …
rm -rf <scratch>               # done — dispose of the scratch
```

On `k`, bulle prints exactly this recipe with the paths and id filled in.

Why this is safe and sufficient:

- During the run, the sandbox denies the origin path, so a push from inside
  the sandbox fails on filesystem permissions. After the run, a push is a
  deliberate act by the user in a trusted shell — the review gate is temporal,
  not mechanical.
- `receive.denyCurrentBranch` refuses pushes to the origin's checked-out
  branch, so the result lands as a ref (`scratch/<id>`), never as
  working-tree changes appearing under the user.
- Conflict handling, three-way merging, and history surgery are git's own —
  strictly better than a bespoke patch-apply, and already in the user's
  muscle memory.
- Agent commits inside the scratch are harmless (the repo is severed) and
  push cleanly; if the agent left dirty state instead, the user commits it
  after review.

## 9. Integration points

- **`cli.Flags`**: add `Scratch bool`, `ScratchKeep bool`. No value parsing;
  reject `--scratch=<anything>` with the worktree hint for `worktree` and a
  generic error otherwise.
- **`app.Run`**: create the scratch after defaults are applied and the command
  is known, but **before** `policy.Resolve`, then set `opts.ProjectPath` to the
  scratch path so `$WORKSPACE` and the automatic read-write grant follow it
  with no changes in `internal/policy`.
- **Cleanup**: unlike shim directories, scratches are *not* removed by the
  deferred cleanup in `app.Run`. Only an empty change set (§8) or an explicit
  discard removes one.
- **`--policy`**: the resolution table shows the scratch path as the workspace
  and names its origin, so `--scratch --policy` explains itself without
  running anything.
- **`--last`**: replays as a *new* scratch rather than reusing the previous
  one, carrying the origin's *current* dirty state (not the state at the
  original run).
- **Denial diagnostics**: hints must report paths relative to the scratch, and
  rewrite scratch paths back to origin-relative form so a suggested `--ro`
  grant is meaningful on the next run.

## 10. Failure modes

| Situation | Behavior |
| --- | --- |
| Not a git repo | Error: scratch requires git; suggest `git init` + initial commit |
| Repo has no commits | Error, same suggestion |
| Origin has submodules | v1: warn that submodules are not carried |
| Dirty-state diff fails to apply | Error before running the command (§6) |
| Scratch creation fails midway | Remove the partial scratch, exit `ExitConfigError` |
| Command times out | Scratch kept; summary still printed |
| Disk full during clone/carry | Remove the partial scratch, report the shortfall |
| Scratch root on a different filesystem than origin | Warn that objects will be copied, not hardlinked; proceed |
| `--scratch` with `--no-workspace` | Error: contradictory |
| Origin inside an existing scratch | Refuse to nest |

## 11. Open questions

1. Should bulle grow a `scratch list` (and `gc`) escape hatch for forgotten
   scratches, or is documenting `$XDG_STATE_HOME/bulle/scratch/` enough? v1
   ships without subcommands; `meta.toml` (§7) keeps the option open.

Resolved:

- Worktree mode is dropped entirely (§3); `--scratch` is a boolean.
- Non-git workspaces are out of scope (§2); copy mode and its exclusion/size
  machinery are gone with them.
- Dirty-state carry is always on (§6); the tri-state flag is gone.
- Apply is replaced by push-based integration (§8.1); the patch/staleness
  machinery is gone.
