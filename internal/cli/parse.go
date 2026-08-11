package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"
)

func Parse(args []string) (Options, error) {
	var opts Options
	if len(args) == 0 {
		return opts, fmt.Errorf("missing argv")
	}
	// help/version aliases are checked before separator inference so that
	// "bulle help" is never mistaken for a sandboxed command named help.
	if len(args) > 1 {
		switch args[1] {
		case "help":
			opts.Help = true
			return opts, nil
		case "version":
			opts.Version = true
			return opts, nil
		}
	}
	cliArgs, command, note := splitCommand(args[1:])
	if note != "" {
		opts.Notes = append(opts.Notes, note)
	}
	cliArgs = normalizeTimeoutValue(cliArgs)
	var policyFormat string
	var err error
	cliArgs, policyFormat, err = normalizePolicyFormat(cliArgs)
	if err != nil {
		return opts, err
	}
	if err := rejectScratchValue(cliArgs); err != nil {
		return opts, err
	}
	var parsed runCLI
	if err := parseKong(&parsed, cliArgs); err != nil {
		return opts, err
	}
	opts.Flags = parsed.Flags
	timeout, err := parseTimeout(parsed.Timeout)
	if err != nil {
		return opts, err
	}
	opts.Timeout = timeout
	opts.PolicyFormat = policyFormat
	if opts.Policy && opts.PolicyFormat == "" {
		opts.PolicyFormat = "summary"
	}
	opts.ProjectPath = parsed.ProjectPath
	if opts.ProjectPath == "" {
		opts.ProjectPath = "."
	}
	opts.Command = command
	return opts, nil
}

type runCLI struct {
	Flags

	ProjectPath string `arg:"" optional:"" name:"workspace" help:"Workspace directory to run from and grant read-write access."`
}

type Flags struct {
	Profile         string `name:"profile" short:"p" placeholder:"NAME" help:"Named profile, or comma-separated profiles merged left to right."`
	Config          string `name:"config" placeholder:"PATH" help:"Path to a configuration directory."`
	InstallProfiles string `name:"install-profiles" placeholder:"SOURCE" help:"Install profile TOML files from a file, directory, local git repository, or GitHub source."`

	ReadOnly      []string `name:"ro" placeholder:"PATH" help:"Grant read-only access."`
	ReadOnlyExec  []string `name:"rox" placeholder:"PATH" help:"Grant read-only access plus execute."`
	ReadWrite     []string `name:"rw" placeholder:"PATH" help:"Grant read-write access."`
	ReadWriteExec []string `name:"rwx" placeholder:"PATH" help:"Grant read-write access plus execute."`

	Env          []string `name:"env" sep:"none" placeholder:"NAME[=VALUE]" help:"Pass NAME (or a NAME glob such as 'GIT_*') from the current environment, or set NAME=VALUE."`
	EnvFile      []string `name:"env-file" sep:"none" placeholder:"PATH" help:"Load NAME=VALUE environment entries from a dotenv-style file."`
	EnvAllExcept []string `name:"env-all-except" placeholder:"NAME,..." help:"Pass the whole parent environment except the named variables."`
	Var          []string `name:"var" sep:"none" placeholder:"NAME=VALUE" help:"Define a custom path variable usable in path grants as $NAME."`

	Help    bool `name:"help" short:"h" help:"Show this help and exit."`
	Version bool `name:"version" short:"V" help:"Show version information and exit."`

	Scratch     bool `name:"scratch" help:"Run against a disposable local clone of the workspace, then review the changes."`
	ScratchKeep bool `name:"scratch-keep" help:"Skip the review prompt, keep the scratch, and print its path."`

	Last       bool `name:"last" help:"Repeat the previous bulle invocation, with any extra flags appended."`
	NoDefaults bool `name:"no-defaults" help:"Ignore the [defaults] block of the user configuration."`

	AddExec       bool   `name:"add-exec" help:"Add the resolved command executable to the sandbox."`
	AddLibs       bool   `name:"add-libs" help:"Add runtime library access for executables."`
	NoWorkspace   bool   `name:"no-workspace" help:"Do not automatically grant the workspace read-write access."`
	ListProfiles  bool   `name:"list-profiles" help:"List available profiles and exit."`
	ListResolvers bool   `name:"list-resolvers" help:"List path resolvers with what they resolve to on this machine, and exit."`
	Timeout       string `name:"timeout" placeholder:"DURATION" help:"Kill the sandboxed command if it runs longer than DURATION, using Go duration syntax such as 30s, 2m, or 1h30m; 0 disables."`
	Policy        bool   `name:"policy" help:"Print the resolved policy and exit."`
}

func normalizePolicyFormat(args []string) ([]string, string, error) {
	out := make([]string, 0, len(args))
	format := ""
	for _, arg := range args {
		value, ok := strings.CutPrefix(arg, "--policy=")
		if !ok {
			out = append(out, arg)
			continue
		}
		if value != "summary" && value != "json" {
			return nil, "", fmt.Errorf("invalid --policy value %q; use summary or json", value)
		}
		if format != "" && format != value {
			return nil, "", fmt.Errorf("conflicting --policy values %q and %q", format, value)
		}
		format = value
		out = append(out, "--policy")
	}
	return out, format, nil
}

// rejectScratchValue turns kong's generic bool-parse failure for
// --scratch=<value> into a message that names the design: scratch has no
// modes, and worktree isolation is deliberately not offered.
func rejectScratchValue(args []string) error {
	for _, arg := range args {
		value, ok := strings.CutPrefix(arg, "--scratch=")
		if !ok {
			continue
		}
		if value == "worktree" {
			return fmt.Errorf("--scratch does not offer a worktree mode: worktrees share the origin's .git (including hooks), which defeats scratch isolation; for trusted parallel sessions use a worktree manager such as wt around bulle")
		}
		return fmt.Errorf("--scratch takes no value; it always clones the workspace")
	}
	return nil
}

func normalizeTimeoutValue(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--timeout" && i+1 < len(args) && strings.HasPrefix(args[i+1], "-") && args[i+1] != "--" {
			out = append(out, "--timeout="+args[i+1])
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func parseKong(grammar any, args []string) error {
	parser, err := kong.New(
		grammar,
		kong.Name("bulle"),
		kong.NoDefaultHelp(),
		kong.Exit(func(int) {}),
		kong.Writers(io.Discard, io.Discard),
	)
	if err != nil {
		return err
	}
	_, err = parser.Parse(args)
	if err != nil {
		return fmt.Errorf("%s (run 'bulle --help')", err)
	}
	return nil
}

func parseTimeout(value string) (time.Duration, error) {
	if value == "" || value == "0" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("invalid --timeout value %q; use a Go duration such as 30s, 2m, or 1h30m", value)
	}
	return duration, nil
}

// NormalizeSeparator returns the argument list (without the program name)
// with an explicit "--" before the command, applying the same split used by
// Parse. Recording invocations in this form lets later flags be inserted
// before the command unambiguously.
func NormalizeSeparator(args []string) []string {
	cliArgs, command, _ := splitCommand(args)
	if len(command) == 0 {
		return cliArgs
	}
	return append(append(cliArgs, "--"), command...)
}

// valueFlags are flags that consume the following argument when not written
// in --flag=value form, so the separator inference below does not mistake
// their values for positionals.
var valueFlags = map[string]bool{
	"--profile": true, "-p": true,
	"--config": true, "--install-profiles": true,
	"--ro": true, "--rox": true, "--rw": true, "--rwx": true,
	"--env": true, "--env-file": true, "--env-all-except": true,
	"--var": true, "--timeout": true,
}

// splitCommand separates bulle's own arguments from the sandboxed command.
// An explicit "--" always wins. Without one, the first positional that is an
// existing directory reads as the workspace (ambiguity resolves toward the
// workspace), and the first positional after that — or a first positional
// that is not an existing directory — starts the command. The returned note
// announces an inferred command split.
func splitCommand(args []string) ([]string, []string, string) {
	for i, arg := range args {
		if arg == "--" {
			return append([]string{}, args[:i]...), append([]string{}, args[i+1:]...), ""
		}
	}
	sawWorkspace := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if valueFlags[arg] && i+1 < len(args) {
				i++
			}
			continue
		}
		if !sawWorkspace {
			if info, err := os.Stat(arg); err == nil && info.IsDir() {
				sawWorkspace = true
				continue
			}
		}
		note := fmt.Sprintf("bulle: treating %q as the start of the command; use -- to separate the command explicitly", arg)
		return append([]string{}, args[:i]...), append([]string{}, args[i:]...), note
	}
	return append([]string{}, args...), nil, ""
}
