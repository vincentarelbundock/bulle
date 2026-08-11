#import "/.calepin/calepin.typ" as calepin

#set document(title: [Diagnostics])
#metadata((
  title: "Diagnostics",
  description: "How bulle reports sandbox denials with copy-pasteable policy fixes, and how to retry the run with the missing grant.",
)) <website-metadata>

#title()

When a sandboxed command fails, the most common question is: _which path did the sandbox block?_ On supported systems, `bulle` answers it for you. After a failed run, it reads the operating system's own record of sandbox denials and prints copy-pasteable fixes:

```text
$ bulle ~/project -- cat ~/.gitconfig
cat: /home/vincent/.gitconfig: Permission denied
bulle: the sandbox denied the following accesses during this run:
  denied: read /home/vincent/.gitconfig — add --ro ~/.gitconfig
```

Add the suggested flag and re-run. No configuration inside `bulle` is needed --- the hints appear automatically whenever the OS makes denial records available, and stay silent otherwise.

These hints are diagnostics only. Enforcement is always done by the kernel (#link("https://landlock.io/")[Landlock] on Linux, Seatbelt on macOS) and works identically whether or not denial logging is available.

= macOS

Nothing to set up. The macOS kernel logs every sandbox violation to the unified log, and `bulle` queries it (`log show`) after a failed run. Two caveats:

- Reading the log takes a few seconds, so hints may appear with a short delay after a failure.
- Violation records are written asynchronously, and the unified log sometimes redacts file paths as `<private>`. Denials from the very end of a run, or with redacted paths, may be missing from the hints.

= Linux

Denial hints need two things, both common but not universal:

+ *A kernel with Landlock audit support* --- Linux 6.15 or newer (Landlock ABI v7). On older kernels, `bulle` enforces the sandbox exactly the same but cannot obtain denial records.
+ *The audit subsystem enabled at runtime.* Some distributions ship it on, others off:

#table(
  columns: 2,
  table.header([Distribution], [To enable denial logging permanently]),
  [Fedora, RHEL, CentOS, Rocky], [Nothing to do --- on by default],
  [openSUSE], [Nothing to do --- on by default],
  [Debian, Ubuntu], [`sudo apt install auditd`],
  [Arch], [`sudo pacman -S audit && sudo systemctl enable --now auditd`],
  [Alpine], [`sudo apk add audit && sudo rc-update add auditd && sudo service auditd start`],
  [NixOS], [Set `security.auditd.enable = true;` and rebuild],
  [Other], [Add `audit=1` to the kernel command line, or run `auditctl -e 1` (until reboot)],
)

`bulle` reads the denial records back through `journalctl`, which requires your user to be able to read the journal. On most distributions the default administrator account already can (via the `adm` or `wheel` group); if not, add yourself to the `systemd-journal` group:

```bash
sudo usermod -aG systemd-journal "$USER"
```

Running `auditd` does not hide records from `bulle`: the audit daemon and the systemd journal receive kernel audit messages independently, so the hints keep working alongside a full auditd setup.

== Inspecting denials manually

The same records are available directly, with or without `bulle`:

```bash
journalctl --quiet --no-pager _TRANSPORT=audit + _TRANSPORT=kernel | grep blockers=
```

Each Landlock denial looks like:

```text
audit: type=1423 audit(1729738800.268:30): domain=195ba459b blockers=fs.read_file path="/home/vincent/.gitconfig" dev="vda2" ino=1523541
```

= How suggestions map to flags

#table(
  columns: 2,
  table.header([Denied operation], [Suggested grant]),
  [read a file or directory], [`--ro PATH`],
  [write, create, delete, or truncate], [`--rw PATH`],
  [execute a program], [`--rox PATH`],
  [outbound or listening network access], [none --- noted as restricted by the network policy],
)

`bulle` deduplicates repeated denials, abbreviates your home directory as `~`, and caps the output at ten hints per run. Hints are only printed after a _failed_ run: a command that succeeds despite probing a blocked path (many tools try optional config files) stays quiet.

#calepin.elements.callout(kind: "warning", title: [Hints are suggestions, not automatic decisions])[
  A denial means the sandbox worked. Before copying a suggested flag, consider whether the command _should_ have that access --- an untrusted tool probing `~/.ssh` is exactly what the sandbox is for.
]

= Rerunning with added grants
<rerun>

A sandboxed run often fails because one grant is missing. Alongside the hints above, `bulle` prints a copy-pasteable retry line:

```text
bulle: the sandbox denied the following accesses during this run:
  denied: read /home/user/.gitconfig — add --ro ~/.gitconfig
bulle: retry with these grants: bulle rerun --ro ~/.gitconfig
```

`bulle rerun` repeats the previous invocation --- from any shell, restoring the original working directory --- and inserts any extra flags before the command, so the retry line works as-is. Each run overwrites the recorded invocation, and repeated `rerun` invocations accumulate their added grants. The sandbox is restarted rather than widened: Landlock cannot extend a live sandbox, and agents resume from their own session state.

The invocation is recorded in `$XDG_STATE_HOME/bulle/last-run.json` (usually `~/.local/state/bulle/`) on Linux and `~/Library/Application Support/bulle/` on macOS.
