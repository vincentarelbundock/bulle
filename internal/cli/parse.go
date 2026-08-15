package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"

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
	cliArgs, command, note := splitCommand(args[1:])
	if note != "" {
		opts.Notes = append(opts.Notes, note)
	}
	cliArgs = normalizeTimeoutValue(cliArgs)
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

	ProjectPath string `arg:"" optional:"" name:"workspace" help:"Workspace directory to run from and grant read-write access."`
}

type Flags struct {
	Profile string `name:"profile" short:"p" placeholder:"NAME" complete:"profile" help:"Named profile, or comma-separated profiles merged left to right."`
	Config  string `name:"config" placeholder:"PATH" complete:"file" help:"Path to a configuration directory."`

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

	AddExec     bool   `name:"add-exec" help:"Add the resolved command executable to the sandbox."`
	AddLibs     bool   `name:"add-libs" help:"Add runtime library access for executables."`
	NoWorkspace bool   `name:"no-workspace" help:"Do not automatically grant the workspace read-write access."`
	Timeout     string `name:"timeout" placeholder:"DURATION" help:"Kill the sandboxed command if it runs longer than DURATION, using Go duration syntax such as 30s, 2m, or 1h30m; 0 disables."`

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
// their values for positionals. Derived from the Flags struct so adding a
// flag there keeps inference (and completion) correct automatically.
var valueFlags = func() map[string]bool {
	m := map[string]bool{}
	for _, f := range GlobalFlags() {
		if !f.TakesValue {
			continue
		}
		m["--"+f.Name] = true
		if f.Short != "" {
			m["-"+f.Short] = true
		}
	}
	return m
}()

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
