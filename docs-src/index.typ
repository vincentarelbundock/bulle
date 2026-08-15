#import "/.calepin/calepin.typ" as calepin

#set document(title: [bulle])
#metadata((
  title: "Home",
  description: "A simple sandbox for dangerous tools like coding agents",
)) <website-metadata>

#html.elem("p", attrs: (style: "text-align: center;"))[
  #image("/assets/bulle.svg", alt: "bulle logo", width: 300pt)
]

#html.elem("p", attrs: (style: "text-align: center; font-size: 1.2em;"))[
  *A simple sandbox for dangerous tools like coding agents*
]

`bulle` is an easy-to-use sandbox for running local commands while exposing only the essential parts of your machine. It allows you to run tools you don't fully trust, without handing over all your files or secrets, and with an option to deny network access. `bulle` sandboxes are especially helpful when running LLM coding agents or untrusted scripts.

You can spin up an agent with restricted permissions using this simple command:

```bash
bulle /path/to/project -- claude
```

`bulle` notices that the built-in `claude` profile is designed to run this command, announces the profile it selected, and applies its permissions automatically. The same works for `codex`, `opencode`, and `pi`.

Sandboxes are not limited to agents. You can use `bulle` to run any command with custom permissions. See the #link("#quick-start")[Quick start] section below, #link("permissions.html")[Permissions] for the grants you write by hand, and the #link("cli-reference.html")[CLI reference] for every flag.

#calepin.elements.callout(kind: "warning", title: none)[
  This software was written for my personal use. You are free to use it, but the license makes no guarantees.

  `bulle` is still experimental. Please report bugs, comments, and feature requests #link("https://github.com/vincentarelbundock/bulle")[on GitHub].
]

= Risk mitigation

`bulle` uses #link("permissions.html#os-level-sandboxing")[Operating System-level sandboxing] to constrain a command's access to paths and environment variables. Like all sandboxing approaches, this strategy imposes trade-offs between convenience and safety. `bulle` will not solve all your security problems, but it can mitigate some important risks.

#calepin.elements.callout(kind: "tip", title: [`bulle` can mitigate risk when])[
  - a prompt or skill injection tells an agent to steal passwords or keys stored outside the sandbox;
  - an LLM agent or script tries to rewrite `~/Documents` instead of the project where it should be running;
  - a malicious package searches your home directory for cloud credentials;
  - a crash log exposes your `API_KEY` environment variable;
  - a tool surreptitiously runs code from downloads, caches, or another project.
]

#calepin.elements.callout(kind: "warning", title: [`bulle` is not sufficient when])[
  - the command needs network access but should not send readable code to a specific service;
  - the command itself needs secrets or paths you cannot afford to leak;
  - you are running code from hostile parties and need a separate machine boundary, not just local OS rules.
]

For more information on sandboxing tradeoffs, read #link("https://www.luiscardoso.dev/blog/sandboxes-for-ai")[A field guide to sandboxes for AI] by Luis Cardoso.

= Install

`bulle` is only available on MacOS and Linux.

With the install script:

```sh
curl -fsSL https://raw.githubusercontent.com/vincentarelbundock/bulle/main/install.sh | sh
```

With Homebrew:

```sh
brew install vincentarelbundock/tap/bulle
```

Or download a prebuilt `darwin`/`linux`, `amd64`/`arm64` archive from the #link("https://github.com/vincentarelbundock/bulle/releases/latest")[latest GitHub release].

= Quick start
<quick-start>

By default, `bulle` runs in the current directory. Access to any other location in the filesystem is denied unless you grant it explicitly. Commands cannot read files, execute programs, or inherit environment variables unless you allow them.

```bash
bulle -- ls
```

```text
command not found before sandbox setup: "ls"
Grant an executable path with --rox/--rwx, choose a profile,
or pass an explicit executable path after --
```

That error is intentional: even finding and executing `ls` requires permission. Grant read-and-execute access to a directory with `--rox`, and `bulle` can find commands in it:

```bash
bulle --rox /bin -- ls
```

Instead of specifying the path of every command manually, we can use #link("profiles.html")[profiles]: named bundles of permissions for common tools. `bulle` ships with built-in profiles for several coding agents, and when your command matches the tool a profile was made for, `bulle` selects that profile for you. The commands below each give read-write access to a workspace and launch an agent with minimal permissions:

```sh
bulle -- claude
# bulle /path/to/project -- claude
# bulle -- codex
# bulle -- pi
# bulle -- opencode
```

`bulle` announces the choice on startup, for example:

```text
bulle: selected profile "claude" because its default_app runs "claude" and
the default profile cannot; pass --profile to choose explicitly
```

You can always pick a profile explicitly instead --- `bulle --profile claude`, which also launches the app for you --- or combine profiles and one-off grants; see #link("profiles.html")[Profiles] for the distinction between selecting profiles and letting `bulle` infer one.

= Where to go next

- #link("permissions.html")[Permissions] --- filesystem and environment grants, network, policy inspection, and how the sandbox is enforced.
- #link("profiles.html")[Profiles] --- named bundles of permissions, the TOML format, portable path entries, and how to write your own.
- #link("scratch.html")[Scratch workspaces] --- run an agent against a disposable clone and review the diff before it touches your checkout.
- #link("denial-diagnostics.html")[Diagnostics] --- what to do when the sandbox blocks something, and how to retry with the missing grant.
- #link("cli-reference.html")[CLI reference] --- the full `bulle --help` output.

= License and attribution

`bulle` is distributed under the MIT License. See #link("LICENSES/bulle-MIT.txt")[LICENSES/bulle-MIT.txt].

Thank you to #link("https://github.com/Zouuup/landrun")[Landrun], an excellent, compact Go implementation of practical Landlock sandboxing. The Linux sandbox backend and filesystem permission model owe a clear debt to Landrun's design, and portions of the Linux backend and ELF dependency discovery are derived from or inspired by Landrun. See #link("LICENSES/landrun-MIT.txt")[LICENSES/landrun-MIT.txt] for the full third-party notice and license.
