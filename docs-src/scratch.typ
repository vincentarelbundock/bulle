#set document(title: [Scratch Workspaces])
#metadata((
  title: "Scratch Workspaces",
  description: "Run an agent against a disposable clone of your repository, then review and integrate the changes with git.",
)) <website-metadata>

#title()

The sandbox limits _where_ a command can write. A scratch limits whether those
writes reach your real checkout at all: the command runs against a disposable
local clone, and its changes become a reviewable diff.

```text
$ bulle scratch claude
bulle: scratch ~/.local/state/bulle/scratch/project-a1b2c3d4 (origin ~/project, from HEAD + 3 modified, 1 untracked)
# ... agent works ...
scratch: 4 files changed, 1 added, 0 deleted
  M	internal/policy/resolve.go
  M	internal/policy/policy.go
  A	internal/policy/scratch.go
[d]iff  [p]ull  [k]eep  [s]hell  [w]ipe?
```

The whole model in three sentences: bulle clones the repository locally
(carrying your uncommitted work), runs the sandboxed command there, and shows
a diff at the end. You pull the changes in, keep the scratch, or wipe it.
Nothing reaches your checkout except a deliberate pull or push from outside
the sandbox.

= Two ways to ask for one

`bulle scratch` is the subcommand form, and the one to reach for: everything
after `scratch` is an ordinary run, so any invocation becomes a scratched one
by inserting a single word.

```text
bulle claude                        # runs in your checkout
bulle scratch claude                # runs in a disposable clone
```

`--scratch` is the same thing as a run flag and composes with policy inspection:

```text
bulle show --scratch claude         # resolve a scratched policy, without running
```

The subcommand's one ambiguity is a command named like a review verb
(#link("#coming-back-later")[`list`, `diff`, `pull`, `wipe`, `shell`]). Those
five words after `scratch` are read as review verbs; anything else starts a
run. Use `--` to be explicit:

```text
bulle scratch -- diff HEAD~1        # run `diff`, do not review
```

A near-miss of a verb is reported rather than run, so a mistyped `bulle
scratch lst` tells you what you meant instead of cloning your repository to
run a nonexistent command. And `--scratch` takes no value: there is no
`--scratch=worktree`, for #link("#why-not-a-worktree")[reasons below].

= How it works

+ *Clone.* `git clone --local --no-hardlinks` creates a self-contained copy
  under `$XDG_STATE_HOME/bulle/scratch/`. Git objects are copied rather than
  hardlinked: the writable scratch can never mutate an object inode shared
  with the origin.
+ *Carry your uncommitted work.* A fresh clone would start at `HEAD`, a
  state you are not looking at. So the scratch always starts from what you
  see: uncommitted tracked changes are applied and untracked, non-ignored
  files are copied. To start from a clean `HEAD` instead, commit or stash
  first. What was carried is reported on stderr.
+ *Run.* The scratch becomes the workspace: `$WORKSPACE` and the automatic
  read-write grant follow it. Your real checkout is *not* granted --- during
  the run, nothing in the sandbox can reach it.
+ *Review.* After the command exits --- on success, failure, and timeout
  alike --- bulle shows what changed and prompts. A run that changed nothing
  removes the scratch and says so in one line. Changes are detected against
  the recorded starting state, so work the agent _committed_ inside the
  scratch is counted too, not just dirty files.

Your shell never leaves your repository: the _command_ runs inside the
scratch (its working directory and `$WORKSPACE` point there), and when it
exits you are back at your own prompt, which never moved. The review summary
and the integration recipe print right there.

At the prompt: `d` pages the full diff and asks again; `p` pulls the
scratch's commits into your repository and removes the scratch on success;
`k` keeps the scratch and prints the integration recipe; `s` keeps it and
opens your shell inside the scratch (unsandboxed --- the run is over; exit to
return to the prompt); `w` wipes it after a confirmation. A subshell is as
close as any program can get to "cd into the scratch": a child process can
never change its parent shell's directory.

== Pushing a scratch to a pull request

Merging into your checkout is not the only way out of a scratch. Opening the
review shell (`s`, or `bulle scratch shell <id>` later) also copies your
repository's remotes into the scratch and prints the push:

```text
remote configured: origin -> git@github.com:you/repo.git
to open a pull request from this scratch:
  git push -u origin HEAD:scratch/4c014a01
  gh pr create --head scratch/4c014a01
```

The clone's own remote --- the local path your scratch came from --- is
renamed to `source` when your repository has an `origin` of its own, so
`git push origin` in that shell means the forge, which is what typing it
there intends. The refspec pushes to a branch named after the scratch rather
than to the branch the scratch has checked out, so a scratch never lands on
`main` by accident.

This happens at review time and never during the run. The confined command
sees a scratch whose only remote is the local clone path: no forge URL, no
credentials, nothing to push with. The shell that _can_ push is unsandboxed
and is you, so the git and `gh` credentials you already have are the ones
that authenticate --- an agent running under `--dangerously-skip-permissions`
never gets near them.

One consequence of the same safety property: the review gate restores the
scratch's git configuration to what git itself wrote at clone time, since the
run could otherwise leave hooks and filters there for your shell to trip
over. Configuration you set by hand inside the review shell --- including the
upstream tracking that `git push -u` records --- does not survive the next
`bulle scratch` command. The remotes are re-derived from your repository each
time, so the push line keeps working.

`p` never commits on your behalf: pull moves commits only, so if the scratch
has uncommitted changes, bulle points you at `s` to commit them (or clean the
tree) yourself --- when you exit the shell you are back at the prompt, ready to
`p`. And if you leave the shell with the scratch back at its starting state,
bulle removes it like any other changeless run.

A failed `p` ends the review rather than looping --- the next steps live in
your repository, not in bulle. Two cases:

- *Merge conflict.* The pull leaves your repository mid-merge, exactly as a
  conflicting `git pull` from any remote would. bulle says so and steps
  aside: resolve the conflicts in your repository (`git status` lists them),
  then `git add` and `git commit`. The scratch is kept until you remove it.
- *Refused up front* (for example, uncommitted changes in your repository
  to the same files the merge would touch). Nothing was merged, both sides
  are untouched, and the integration recipe is printed for once you have
  dealt with the obstacle.

Without a terminal, or with `--scratch-keep`, the scratch is kept and the
recipe printed. *A scratch is never deleted implicitly* --- not on timeout,
signal, or crash. Losing an agent's work to a cleanup path is worse than
leaving a directory behind.

= Integrating the changes

There is no bespoke apply step. The scratch is a real repository, so
integration is ordinary git, done by you, outside the sandbox. When you are
confident in the changes, one command from your repository pulls them in:

```text
git add -A && git commit          # in the scratch, if anything is uncommitted
git -C ~/project pull ~/.local/state/bulle/scratch/project-a1b2c3d4
rm -rf ~/.local/state/bulle/scratch/project-a1b2c3d4
```

`k` prints this recipe with your paths filled in, and checks the scratch
first: pull only moves _commits_, so if the scratch has uncommitted changes
the recipe warns and leads with the commit step rather than letting you
silently integrate a partial result.

To inspect before merging, fetch the scratch into a ref instead and take it
from there with your usual tools:

```text
git -C ~/project fetch ~/.local/state/bulle/scratch/project-a1b2c3d4 HEAD:scratch/a1b2c3d4
git diff main...scratch/a1b2c3d4
git merge scratch/a1b2c3d4        # or rebase, cherry-pick
```

This is safe by construction. During the run, the sandbox denies your
checkout's path, so nothing inside the sandbox can reach your repository at
all; after the run, a pull is a deliberate act in your trusted shell, and the
merge runs in your checkout so the working tree updates properly. (Pushing
from the scratch to your checked-out branch would be refused by git for
exactly that reason --- a push updates refs, never your working tree.)

= Why not a worktree?

Tools like Worktrunk's `wt` already automate "run an agent in a separate
directory" with `git worktree add`. They are good at what they do --- templated
paths, setup hooks, a merge command --- and they compose with bulle today:
`wt switch -c feature-x` then `bulle claude` inside the worktree.

But a worktree's `.git` is a pointer file into your real repository. For git
to work there at all, the sandbox would have to grant read-write access to
your repository's `.git` --- objects, refs, config, and `.git/hooks`, which all
worktrees share. An untrusted agent could write a `pre-commit` hook that runs
on _your_ next commit, outside the sandbox. That re-exposes exactly what
scratch exists to contain, which is why `--scratch=worktree` is not offered.
`git clone --local --no-hardlinks` gives the same separate-directory experience with a
genuinely severed `.git`: hooks are not carried over, and no write access to
your repository is needed.

The rule of thumb: for parallel _trusted_ sessions, use a worktree manager
around bulle; for reviewable _untrusted_ runs, use `--scratch`.

= Coming back later

A kept scratch --- or one left behind by a failed pull --- is a paused review,
and `bulle scratch` resumes it:

```text
bulle scratch list           # id, age, change summary, origin
bulle scratch diff [id]      # the review diff against the recorded baseline
bulle scratch pull [id]      # same semantics as [p] at the prompt
bulle scratch wipe [id]      # same as [w], with a confirmation
bulle scratch shell [id]     # same as [s]
```

The verbs mean exactly what the prompt letters mean --- one mental model, two
entry points. The id may be any unique prefix, and may be omitted when there
is only one scratch (or only one whose origin is the current directory).
Deliberately absent: `gc` --- bulk cleanup is `list` plus `wipe`, and an
auto-deletion policy is where "a scratch is never deleted implicitly" would
go to die.

= Details and edge cases

- *Git only.* A scratch requires a git repository with at least one
  commit. For a plain directory, `git init && git add -A && git commit` takes
  three commands and also buys you the review diff.
- *Location.* Scratches live under `$XDG_STATE_HOME/bulle/scratch/` --- not
  inside your repository (where `git status`, indexers, and build tools would
  pick them up) and not in `/tmp` (whose cleaners are not patient enough to
  wait for your review). Override with `[scratch] dir = "..."` in
  `config.toml`, useful when your repositories live on a different filesystem
  than your state directory. Object files are copied on every filesystem;
  placing the scratch root near repositories can still reduce other I/O costs.
- *Metadata.* A `meta.toml` beside each scratch records its origin, base
  commit, and the invocation that created it.
- *Denial hints* report paths in origin-relative form, so a suggested
  `--ro` grant from a scratch run is meaningful on the next one.
- *Submodules* are not carried into the scratch (bulle warns when the
  origin has them).
- *`bulle show --scratch PROFILE`* prints the scratch-to-origin mapping and the
  resolved policy without running anything, then removes the unused clone.
