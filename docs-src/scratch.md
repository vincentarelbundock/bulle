---
title: Scratch Workspaces
description: Run an agent against a disposable clone of your repository, then review and integrate the changes with git.
hide:
  - navigation
---

# Scratch Workspaces

The sandbox limits *where* a command can write. `--scratch` limits whether
those writes reach your real checkout at all: the command runs against a
disposable local clone, and its changes become a reviewable diff.

~~~text
$ bulle --scratch --profile claude
bulle: scratch ~/.local/state/bulle/scratch/project-a1b2c3d4 (origin ~/project, from HEAD + 3 modified, 1 untracked)
# ... agent works ...
scratch: 4 files changed, 1 added, 0 deleted
  M	internal/policy/resolve.go
  M	internal/policy/policy.go
  A	internal/policy/scratch.go
[d]iff  [k]eep  [s]hell  [D]iscard?
~~~

The whole model in three sentences: `--scratch` clones the repository locally
(carrying your uncommitted work), runs the sandboxed command there, and shows
a diff at the end. You keep or discard. Nothing reaches your checkout except a
deliberate `git push` from outside the sandbox.

## How it works

1. **Clone.** `git clone --local` creates a self-contained copy under
   `$XDG_STATE_HOME/bulle/scratch/`. Git objects are hardlinked, not copied,
   so cloning is nearly free even on repositories with deep history; only the
   working-tree checkout costs real disk.
2. **Carry your uncommitted work.** A fresh clone would start at `HEAD`, a
   state you are not looking at. So the scratch always starts from what you
   see: uncommitted tracked changes are applied and untracked, non-ignored
   files are copied. To start from a clean `HEAD` instead, commit or stash
   first. What was carried is reported on stderr.
3. **Run.** The scratch becomes the workspace: `$WORKSPACE` and the automatic
   read-write grant follow it. Your real checkout is **not** granted — during
   the run, nothing in the sandbox can reach it.
4. **Review.** After the command exits — on success, failure, and timeout
   alike — bulle shows what changed and prompts. A run that changed nothing
   removes the scratch and says so in one line. Changes are detected against
   the recorded starting state, so work the agent *committed* inside the
   scratch is counted too, not just dirty files.

Your shell never leaves your repository: the *command* runs inside the
scratch (its working directory and `$WORKSPACE` point there), and when it
exits you are back at your own prompt, which never moved. The review summary
and the integration recipe print right there.

At the prompt: `d` pages the full diff and asks again, `k` keeps the scratch
and prints the integration recipe, `s` keeps it and opens your shell inside
the scratch (unsandboxed — the run is over; exit to return to where you
were), and `D` discards it after a confirmation. A subshell is as close as
any program can get to "cd into the scratch": a child process can never
change its parent shell's directory.
Without a terminal, or with `--scratch-keep`, the scratch is kept and the
recipe printed. **A scratch is never deleted implicitly** — not on timeout,
signal, or crash. Losing an agent's work to a cleanup path is worse than
leaving a directory behind.

## Integrating the changes

There is no bespoke apply step. The scratch is a real repository whose
`origin` remote points at your checkout, so integration is ordinary git, done
by you, outside the sandbox:

~~~text
cd ~/.local/state/bulle/scratch/project-a1b2c3d4
git diff                          # review again if desired
git add -A && git commit
git push origin HEAD:scratch/a1b2c3d4
cd ~/project
git merge scratch/a1b2c3d4        # or rebase, cherry-pick, diff first
rm -rf ~/.local/state/bulle/scratch/project-a1b2c3d4
~~~

`k` prints exactly this recipe with your paths filled in.

This is safe by construction. During the run, the sandbox denies your
checkout's path, so a push from inside fails on filesystem permissions; after
the run, a push is a deliberate act in your trusted shell. And git refuses
pushes to the checked-out branch, so the result always lands as a ref —
never as files silently appearing in your working tree.

## Why not a worktree?

Tools like Worktrunk's `wt` already automate "run an agent in a separate
directory" with `git worktree add`. They are good at what they do — templated
paths, setup hooks, a merge command — and they compose with bulle today:
`wt switch -c feature-x` then `bulle --profile claude` inside the worktree.

But a worktree's `.git` is a pointer file into your real repository. For git
to work there at all, the sandbox would have to grant read-write access to
your repository's `.git` — objects, refs, config, and `.git/hooks`, which all
worktrees share. An untrusted agent could write a `pre-commit` hook that runs
on *your* next commit, outside the sandbox. That re-exposes exactly what
scratch exists to contain, which is why `--scratch=worktree` is not offered.
`git clone --local` gives the same separate-directory experience with a
genuinely severed `.git`: hooks are not carried over, and no write access to
your repository is needed.

The rule of thumb: for parallel *trusted* sessions, use a worktree manager
around bulle; for reviewable *untrusted* runs, use `--scratch`.

## Details and edge cases

- **Git only.** `--scratch` requires a git repository with at least one
  commit. For a plain directory, `git init && git add -A && git commit` takes
  three commands and also buys you the review diff.
- **Location.** Scratches live under `$XDG_STATE_HOME/bulle/scratch/` — not
  inside your repository (where `git status`, indexers, and build tools would
  pick them up) and not in `/tmp` (whose cleaners are not patient enough to
  wait for your review). Override with `[scratch] dir = "..."` in
  `config.toml`, useful when your repositories live on a different filesystem
  than your state directory: hardlinks cannot cross filesystems, and bulle
  warns when objects would be copied instead.
- **Metadata.** A `meta.toml` beside each scratch records its origin, base
  commit, and the invocation that created it.
- **Denial hints** report paths in origin-relative form, so a suggested
  `--ro` grant from a scratch run is meaningful on the next one.
- **`--last`** replays as a *new* scratch carrying your current uncommitted
  state, rather than reusing the previous one.
- **Submodules** are not carried into the scratch (bulle warns when the
  origin has them).
- **`--scratch --policy`** prints the scratch-to-origin mapping and the
  resolved policy without running anything, then removes the unused clone.
