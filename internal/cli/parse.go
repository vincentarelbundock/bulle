package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"
)

func Parse(args []string) (Options, error) {
	var opts Options
	if len(args) == 0 {
		return opts, fmt.Errorf("missing argv")
	}
	cliArgs, command := splitCommand(args[1:])
	var policyFormat string
	var err error
	cliArgs, policyFormat, err = normalizePolicyFormat(cliArgs)
	if err != nil {
		return opts, err
	}
	// Top-level aliases are handled before Kong because the main invocation has
	// no explicit subcommand; adding Kong commands would make help/version
	// ambiguous with the optional workspace argument.
	if len(cliArgs) > 0 {
		switch cliArgs[0] {
		case "help":
			opts.Help = true
			return opts, nil
		case "version":
			opts.Version = true
			return opts, nil
		}
	}
	var parsed runCLI
	if err := parseKong(&parsed, cliArgs); err != nil {
		return opts, err
	}
	opts.Flags = parsed.Flags
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
	Profile string `name:"profile" short:"p" placeholder:"NAME" help:"Named profile from the configuration file."`
	Config  string `name:"config" placeholder:"PATH" help:"Path to a configuration file."`

	ReadOnly      []string `name:"ro" placeholder:"PATH" help:"Grant read-only access."`
	ReadOnlyExec  []string `name:"rox" placeholder:"PATH" help:"Grant read-only access plus execute."`
	ReadWrite     []string `name:"rw" placeholder:"PATH" help:"Grant read-write access."`
	ReadWriteExec []string `name:"rwx" placeholder:"PATH" help:"Grant read-write access plus execute."`

	Env []string `name:"env" sep:"none" placeholder:"NAME[=VALUE]" help:"Pass NAME from the current environment, or set NAME=VALUE."`

	Help    bool `name:"help" short:"h" help:"Show this help and exit."`
	Version bool `name:"version" short:"V" help:"Show version information and exit."`

	AddExec     bool `name:"add-exec" help:"Add the resolved command executable to the sandbox."`
	AddLibs     bool `name:"add-libs" help:"Add runtime library access for executables."`
	NoWorkspace bool `name:"no-workspace" help:"Do not automatically grant the workspace read-write access."`
	NoNetwork   bool `name:"no-network" help:"Deny network access for this invocation."`
	Policy      bool `name:"policy" help:"Print the resolved policy and exit."`
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

func splitCommand(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return append([]string{}, args[:i]...), append([]string{}, args[i+1:]...)
		}
	}
	return append([]string{}, args...), nil
}
