package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"github.com/vincentarelbundock/bulle/internal/didyoumean"
	"github.com/vincentarelbundock/bulle/internal/limits"
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
	cliArgs, command := splitCommand(args[1:])
	cliArgs = normalizeTimeoutValue(cliArgs)
	if err := rejectScratchValue(cliArgs); err != nil {
		return opts, err
	}
	var parsed runCLI
	if err := parseKong(&parsed, cliArgs); err != nil {
		return opts, err
	}
	opts.Flags = parsed.Flags
	opts.Profile = parsed.Profile
	timeout, err := parseTimeout(parsed.Timeout)
	if err != nil {
		return opts, err
	}
	opts.Timeout = timeout
	// Parse the flags on their own so an invalid value is rejected even when
	// no configuration is loaded; applyConfigDefaults re-parses once the
	// [defaults] block has been layered underneath.
	if opts.Limits, err = limits.Parse(parsed.Flags.LimitSpec()); err != nil {
		return opts, err
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

	Profile     string `arg:"" optional:"" name:"profile" help:"Named profile, or comma-separated profiles merged left to right."`
	ProjectPath string `arg:"" optional:"" name:"workspace" help:"Workspace directory to run from and grant read-write access."`
}

type Flags struct {
	Config string `name:"config" placeholder:"PATH" complete:"file" help:"Path to a configuration directory."`

	ReadOnly      []string `name:"ro" placeholder:"PATH" complete:"file" help:"Grant read-only access."`
	ReadOnlyExec  []string `name:"rox" placeholder:"PATH" complete:"file" help:"Grant read-only access plus execute."`
	ReadWrite     []string `name:"rw" placeholder:"PATH" complete:"file" help:"Grant read-write access."`
	ReadWriteExec []string `name:"rwx" placeholder:"PATH" complete:"file" help:"Grant read-write access plus execute."`

	Env          []string `name:"env" sep:"none" placeholder:"NAME[=VALUE]" help:"Pass NAME (or a NAME glob such as 'GIT_*') from the current environment, or set NAME=VALUE."`
	EnvFile      []string `name:"env-file" sep:"none" placeholder:"PATH" complete:"file" help:"Load NAME=VALUE environment entries from a dotenv-style file."`
	EnvAllExcept []string `name:"env-all-except" placeholder:"NAME,..." help:"Pass the whole parent environment except the named variables."`
	Var          []string `name:"var" sep:"none" placeholder:"NAME=VALUE" help:"Define a custom path variable usable in path grants as $NAME."`

	Help    bool `name:"help" short:"h" help:"Show this help and exit."`
	Version bool `name:"version" short:"V" help:"Show version information and exit."`

	Scratch     bool `name:"scratch" help:"Run against a disposable local clone of the workspace, then review the changes."`
	ScratchKeep bool `name:"scratch-keep" help:"Skip the review prompt, keep the scratch, and print its path."`

	NoDefaults bool `name:"no-defaults" help:"Ignore the [defaults] block of the user configuration."`

	NoWorkspace bool   `name:"no-workspace" help:"Do not automatically grant the workspace read-write access."`
	Timeout     string `name:"timeout" placeholder:"DURATION" help:"Kill the whole sandboxed process tree after DURATION (Linux with a delegated cgroup; unavailable on macOS); 0 disables."`

	Memory  string `name:"memory" placeholder:"SIZE" help:"Cap the sandbox's resident memory, as in 512M or 4G. Linux only (cgroup v2)."`
	CPU     string `name:"cpu" placeholder:"PERCENT" help:"Cap the sandbox's CPU use as a percentage of one core, as in 200% for two cores. Linux only (cgroup v2)."`
	NProc   string `name:"nproc" placeholder:"N" help:"Cap the number of processes in the sandbox. Linux only (cgroup v2)."`
	NoFile  string `name:"nofile" placeholder:"N" help:"Cap the number of open file descriptors."`
	FSize   string `name:"fsize" placeholder:"SIZE" help:"Cap the size of any single file the sandbox writes, as in 100M."`
	CPUTime string `name:"cpu-time" placeholder:"DURATION" help:"Cap consumed CPU time (not wall clock), using Go duration syntax. Applies per process rather than to the whole tree."`

	StrictLimits bool `name:"strict-limits" help:"Refuse to run when a requested resource limit cannot be enforced here, instead of warning."`
}

// LimitSpec collects the resource-limit flags in the form the limits package
// merges and parses.
func (f Flags) LimitSpec() limits.Spec {
	return limits.Spec{
		Memory:  f.Memory,
		CPU:     f.CPU,
		NProc:   f.NProc,
		NoFile:  f.NoFile,
		FSize:   f.FSize,
		CPUTime: f.CPUTime,
	}
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
		return decorateParseError(err)
	}
	return nil
}

// decorateParseError turns kong's terse parse failures into messages that
// point at the specific flag: a suggestion for a misspelled flag name, or the
// flag's own syntax line when its value was rejected — instead of referring
// every error to the full help text.
func decorateParseError(err error) error {
	msg := err.Error()
	flags := GlobalFlags()
	if name, ok := strings.CutPrefix(msg, "unknown flag "); ok {
		// kong suggests near misses itself; keep its message when it did.
		if strings.Contains(msg, "did you mean") {
			return err
		}
		name, _, _ = strings.Cut(name, ",")
		names := make([]string, 0, len(flags))
		for _, f := range flags {
			names = append(names, "--"+f.Name)
		}
		if suggestion := didyoumean.Closest(name, names); suggestion != "" {
			return fmt.Errorf("%s (did you mean %s?)", msg, suggestion)
		}
		return fmt.Errorf("%s (run 'bulle --help' for the flag list)", msg)
	}
	// Attribute the error to the first flag the message mentions, which is the
	// one kong is complaining about — a later mention is usually the rejected
	// value ("--nofile: expected a value, got --memory"), and pointing the user
	// at that one sends them to the wrong flag's usage. Ties at the same
	// position go to the longest name, so "--rox" is not mistaken for "--ro".
	var match *FlagSpec
	matchAt := -1
	for i := range flags {
		f := &flags[i]
		at := strings.Index(msg, "--"+f.Name)
		if at < 0 {
			continue
		}
		if match == nil || at < matchAt || (at == matchAt && len(f.Name) > len(match.Name)) {
			match, matchAt = f, at
		}
	}
	if match != nil {
		usage := "--" + match.Name
		if match.Placeholder != "" {
			usage += " " + match.Placeholder
		}
		return fmt.Errorf("%s\nusage: %s\n  %s", msg, usage, match.Help)
	}
	return fmt.Errorf("%s (run 'bulle --help')", msg)
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
// unchanged apart from copying: the grammar requires an explicit "--" before
// any command, so there is nothing to normalize. Kept as the seam scratch
// metadata records invocations through.
func NormalizeSeparator(args []string) []string {
	return append([]string{}, args...)
}

// splitCommand separates bulle's own arguments from the sandboxed command at
// the explicit "--". Everything before it is policy; everything after it is
// the command. There is no inference: a run without "--" has no command and
// falls back to the profile's default_app.
func splitCommand(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return append([]string{}, args[:i]...), append([]string{}, args[i+1:]...)
		}
	}
	return append([]string{}, args...), nil
}
