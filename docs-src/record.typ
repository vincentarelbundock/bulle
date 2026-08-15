#set document(title: [Recording Profiles])
#metadata((
  title: "Recording Profiles",
  description: "Draft a profile for a new tool by running it under observation: bulle collects what the sandbox denied, adds those grants, and runs again until nothing new is denied.",
)) <website-metadata>

#title()

Writing a profile for a new tool means running it, reading the denial, adding a
grant, and running it again --- often a dozen times. `bulle record` does that
loop for you.

```text
$ bulle record --profile tool -- go version
bulle: record: round 1
go version go1.26.4 linux/amd64
bulle: record: command succeeded; 1 grants recorded over 1 round(s)
# Recorded by bulle 0.0.7 on 2026-08-15.
# Command: go version
# Rounds: 1; command exited 0.
...
inherits = ["tool"]

ro = [
  "?/nix/store/gb0njhqswlc5n127ikgyikvq39r40l6f-go-1.26.4",
]
```

Each round runs the command under the base profile plus everything earlier
rounds were denied. When a round adds nothing new, recording stops and prints a
profile that inherits from the base and contains only the additions.

= What a recording proves

*That one run of one command needed these grants. Not that they are
sufficient.*

This is the limitation to keep in mind, and it follows from how enforcement
works. A denial does not merely get logged --- it fails the operation that hit
it. The command then does whatever it does when a file is unavailable: give up,
skip an optional feature, or take an error path. So each round observes only as
far as the command got before the denial stopped it, and anything the run never
reached contributes nothing.

A recorded profile is therefore missing:

- features the command has but this invocation did not use,
- error and recovery paths,
- anything behind a flag, subcommand, or configuration this run did not exercise,
- files that appear only on a first run, or only after a cache expires.

Recording an agent once and assuming the profile covers every future session is
exactly the mistake this warning exists to prevent. Treat the output as a first
draft that ran once, and read every line before installing it. The emitted
profile repeats this in its own header, so it travels with the file.

== What recording cannot see

Recording discovers _paths_. It cannot discover environment variables, and the
difference matters more often than it sounds.

`bulle` passes only the variables a profile's `env` list names. A command that
needs one it does not have --- `DISPLAY` for a graphical program, `HOME` for a
toolchain that keeps a cache there --- fails at startup, and the kernel logs
nothing, because nothing was denied. There is no record for recording to read.

This is the most common way a recording comes back empty:

```text
$ bulle record -p tool -- mygui
bulle: record: round 1
mygui: neither WAYLAND_DISPLAY nor DISPLAY is set
bulle: record: round 1 added no grants but the command still failed with exit 1
bulle: record: no grant will fix that; recording what was learned so far
bulle: record: the sandbox refused nothing this round, so the failure is not about access
bulle: record: a command that fails without any denial is most often missing an environment variable:
bulle: record: bulle passes only what the profile's env list names, and recording cannot discover that
bulle: record: if the command needs one, add it and record again (e.g. --env DISPLAY)
```

Add the variables the command needs, then record again. Everything after the
base profile is an ordinary run, so they go on the same line:

```bash
bulle record -p tool -- --env DISPLAY --env WAYLAND_DISPLAY -- mygui
```

Once the command gets past its startup checks, recording can do its job on the
paths behind them.

= Usage

```text
bulle record --profile NAME [--out FILE] [--max-rounds N] -- command [args...]
```

/ `--profile NAME`: the base profile to record against. Required --- see
  #link(<why-a-base>)[Why a base profile is required].
/ `--out FILE`: write the profile to `FILE` instead of standard output. The
  file is never placed in your profile directory; installing is a separate,
  deliberate step.
/ `--max-rounds N`: stop after `N` rounds (default 10).

Anything after the base profile is an ordinary run, so run flags pass through
untouched:

```bash
bulle record --profile tool --out draft.toml -- --no-network -- mytool build
```

Recording works on Linux and macOS, with one difference in what the output can
promise --- see #link(<attribution>)[Attribution on macOS].

== How recording stops

Three ways, and the narration always says which:

/ The command succeeded: nothing more to learn from this invocation.
/ A round added no grants but the command still failed: it is failing for a
  reason no grant will fix --- a missing file, an unset environment variable, a
  genuine bug. The profile says the entries may be necessary but are
  demonstrably not sufficient.
/ The round cap was reached: the profile is marked `INCOMPLETE`. Re-run with a
  higher `--max-rounds`.

= What gets written

A recorded entry is not the path that was denied. That path is specific to your
machine, and a profile full of them is useless anywhere else. Denied paths are
rewritten into the spelling a hand-written profile would use:

#table(
  columns: 2,
  table.header([Denied here], [Recorded as]),
  [`/home/you/go/pkg/mod/github.com/x/y@v1/go.mod`], [`?go:modcache`],
  [`/home/you/.config/tool/settings.yaml`], [`?$CONFIG/tool/settings.yaml`],
  [`/usr/local/bin/pandoc` (executed)], [`?which:pandoc`],
  [`/nix/store/abc-r-4.5/lib/R/bin/exec/R`], [`?/nix/store/abc-r-4.5`],
)

Every entry is optional (the leading `?`), because a path this machine has is
not one the next machine has, and a profile that fails to resolve is worse than
one that grants nothing. Where several denied files share a directory, the
entries collapse into one directory grant.

Two rewrites deliberately grant more than the denial asked for, and both say so
in a comment beside the entry:

- *A tool resolver* such as `go:modcache` names a whole directory, because the
  profile syntax has no way to name one file inside one. This is the same
  idiom the built-in profiles already use: a tool's cache is used as a whole.
- *A whole-`/proc` grant* appears when a per-process entry like
  `/proc/1234/cgroup` is denied. The pid differs for every child, so no
  narrower grant covers it. This lets the sandboxed command read other same-uid
  processes' `/proc` entries --- a real tradeoff, stated in the profile.

What recording will *not* do is grant a variable root. A denial on `$HOME`,
`$CACHE`, or `/etc` itself stays a literal path in the output, and clusters of
files directly inside such a root never collapse upward into it. Those are the
trees a sandbox exists to withhold, so widening to them is a decision for you,
not for a tool.

= Attribution on macOS <attribution>

On Linux, a Landlock denial is recorded against the sandbox domain, so every
denial `bulle` reads back belongs to the run it is observing.

macOS gives less. The unified log records sandbox violations from _every_
sandboxed process on the machine, and a violation names a pid that has already
exited by the time the log is read --- so it cannot be traced back to this
run's process group. A recording made while Spotlight or some other sandboxed
daemon happens to be denied something may contain that denial too.

`bulle` does not guess. Filtering by process name would look tidier and would
silently drop real grants, because a command's helper processes have different
names than the command. Instead, every recorded entry names the process it was
denied to:

```toml
ro = [
  # denied to mytool
  "?$CONFIG/mytool/settings.yaml",
  # denied to mdworker_shared
  "?/Library/Spotlight",
]
```

The second entry is not yours. Delete it. The emitted profile carries this
warning in its header, so it stays with the file.

Recording on a quiet machine produces less of this. Recording while a backup,
an indexer, or an App Store update is running produces more.

= Why a base profile is required <why-a-base>

Recording diffs against the policy actually in effect, so the output contains
only what the base was missing. Recording against `claude` does not restate
everything `tool`, `terminal`, `git`, and `node` already grant --- and the
result is a small, readable profile you can inherit from the base.

Recording with no base at all is not supported. It is the case where the run
needs the widest grant and the output is least trustworthy, so there is nothing
to check the result against.

= Requirements

Recording needs Landlock audit logging that actually reaches the kernel log ---
the same mechanism behind #link("denial-diagnostics.html")[denial diagnostics],
with the same setup. See that page for the per-distribution table.

`bulle` verifies this before recording rather than trusting the kernel's
advertised capabilities. It runs a small sandboxed command, deliberately
triggers a denial, and confirms the record appears. If it does not, recording
refuses to start:

```text
$ bulle record --profile tool -- mytool
bulle: this kernel reports Landlock audit support, but a denial deliberately
triggered here never reached the log; recording would observe nothing and
produce an empty profile that looks like success. Auditing is most often
disabled at boot (audit=0) or filtered by an audit rule
```

The check exists because the failure it prevents is silent. A kernel can
advertise Landlock ABI v7 while auditing is switched off; recording would then
see no denials, stop after one round, and emit an empty profile --- which reads
exactly like a run that needed nothing.

= After recording

The output is a draft, not an installed profile. Read it, cut what the command
does not need, and add what you know it needs but this run did not reach. Then
save it into your profile directory and use it by name:

```bash
bulle record --profile tool --out ~/.config/bulle/profiles/mytool.toml -- mytool build
$EDITOR ~/.config/bulle/profiles/mytool.toml
bulle --profile mytool -- mytool build
```

Exercise the new profile on the paths the recording missed --- the other
subcommands, the error cases, a cold cache --- and record again against your
own profile to pick up what those runs need. Recording against a profile you
are iterating on is the normal workflow, not a special case.
