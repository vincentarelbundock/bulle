package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	"github.com/vincentarelbundock/bulle/internal/exitcode"
)

// A subcommand ties a CommandSpec to its handler. Run dispatches from this
// table and `bulle __complete` answers from the same specs, so the dispatcher
// and shell completion cannot disagree about what exists.
type subcommand struct {
	cli.CommandSpec
	run func(argv0 string, rest []string, stdout, stderr io.Writer) int
	// helpWhenBare prints the subcommand's help when it is invoked with no
	// arguments at all. Set only where a bare invocation has no meaning of
	// its own — bare rerun, resolvers, policy, and scratch all do something.
	helpWhenBare bool
}

func subcommands() []subcommand {
	subs := []subcommand{
		{cli.CommandSpec{Name: "scratch", Verbs: []string{"list", "diff", "pull", "wipe", "shell"}}, runScratchDispatch, false},
		{cli.CommandSpec{Name: "show", Verbs: []string{"policy", "profiles", "resolvers", "config"}, Extra: []cli.FlagSpec{
			{Name: "json", Help: "Print the resolved policy as JSON."},
		}}, runShowDispatch, false},
		{cli.CommandSpec{Name: "profiles", Verbs: []string{"list", "install"}}, runProfilesDispatch, true},
		{cli.CommandSpec{Name: "completion", Verbs: []string{"bash", "zsh", "fish"}}, runCompletionDispatch, true},
		{cli.CommandSpec{Name: "help"}, runHelpDispatch, false},
		{cli.CommandSpec{Name: "version"}, runVersionDispatch, false},
		{cli.CommandSpec{Name: "__complete", Hidden: true}, runCompleteDispatch, false},
		{cli.CommandSpec{Name: "__man", Hidden: true}, runManDispatch, false},
	}
	// `bulle help <topic>` completes the other subcommand names and the extra
	// topics as verbs.
	for i := range subs {
		if subs[i].Name == "help" {
			for _, sub := range subs {
				if !sub.Hidden && sub.Name != "help" {
					subs[i].Verbs = append(subs[i].Verbs, sub.Name)
				}
			}
			subs[i].Verbs = append(subs[i].Verbs, cli.HelpTopics()...)
		}
	}
	return subs
}

// wantsHelp reports whether rest asks for help before the -- separator;
// after the separator, --help belongs to the sandboxed command.
func wantsHelp(rest []string) bool {
	for _, arg := range rest {
		if arg == "--" {
			return false
		}
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printCommandHelp(name string, stdout io.Writer) int {
	if text, ok := cli.CommandHelp(name); ok {
		fmt.Fprint(stdout, text)
		return exitcode.OK
	}
	fmt.Fprint(stdout, cli.Usage())
	return exitcode.OK
}

func commandSpecs() []cli.CommandSpec {
	subs := subcommands()
	specs := make([]cli.CommandSpec, len(subs))
	for i, sub := range subs {
		specs[i] = sub.CommandSpec
	}
	return specs
}

func runScratchDispatch(argv0 string, rest []string, stdout, stderr io.Writer) int {
	// `bulle scratch` covers the whole lifecycle: the review verbs resume a
	// kept scratch, and anything else creates one, so the subcommand is not
	// limited to scratches that already exist.
	isRun, err := scratchArgsStartRun(rest)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		return exitcode.ConfigError
	}
	if isRun {
		runArgs := append([]string{argv0, "--scratch"}, rest...)
		return runMain(runArgs, "", stdout, stderr, newRecorder())
	}
	return runScratchCommand(rest, stdout, stderr)
}

// runShowDispatch handles `bulle show [what]`: inspection without running
// anything. Bare show and `show policy` resolve and print the policy the same
// arguments would run under; the other verbs report on this machine's
// configuration.
func runShowDispatch(argv0 string, rest []string, stdout, stderr io.Writer) int {
	what := "policy"
	if len(rest) > 0 {
		switch rest[0] {
		case "policy", "profiles", "resolvers", "config":
			what, rest = rest[0], rest[1:]
		}
	}
	switch what {
	case "profiles":
		if len(rest) > 0 {
			fmt.Fprintln(stderr, "usage: bulle show profiles")
			return exitcode.ConfigError
		}
		return runProfilesCommand([]string{"list"}, stdout, stderr)
	case "resolvers":
		if len(rest) > 0 {
			fmt.Fprintln(stderr, "usage: bulle show resolvers")
			return exitcode.ConfigError
		}
		writeResolverListing(parentEnv(), stdout)
		return exitcode.OK
	case "config":
		return runConfigCommand(rest, stdout, stderr)
	default:
		runArgs, format, err := extractPolicyFormat(rest)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitcode.ConfigError
		}
		return runMain(append([]string{argv0}, runArgs...), format, stdout, stderr, nil)
	}
}

func runProfilesDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	return runProfilesCommand(rest, stdout, stderr)
}

func runCompletionDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "usage: bulle completion bash|zsh|fish")
		return exitcode.ConfigError
	}
	script, err := cli.CompletionScript(rest[0])
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		return exitcode.ConfigError
	}
	fmt.Fprint(stdout, script)
	return exitcode.OK
}

func runHelpDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) == 0 {
		fmt.Fprint(stdout, cli.Usage())
		return exitcode.OK
	}
	if text, ok := cli.CommandHelp(rest[0]); ok {
		fmt.Fprint(stdout, text)
		return exitcode.OK
	}
	var topics []string
	for _, sub := range subcommands() {
		if !sub.Hidden {
			topics = append(topics, sub.Name)
		}
	}
	fmt.Fprintf(stderr, "bulle: no help topic %q; topics: %s\n", rest[0], strings.Join(topics, ", "))
	return exitcode.ConfigError
}

// runManDispatch prints the bulle.1 man page. Hidden: it exists for the
// release pipeline and packagers (`bulle __man > bulle.1`), assembled from
// the same strings the terminal help prints.
func runManDispatch(_ string, _ []string, stdout, _ io.Writer) int {
	fmt.Fprint(stdout, cli.ManPage(Version))
	return exitcode.OK
}

func runVersionDispatch(_ string, _ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "bulle %s\n", Version)
	return exitcode.OK
}

// runCompleteDispatch answers shell completion requests from the shims
// printed by `bulle completion`. The words after -- are the current command
// line minus the program name, with the word under the cursor last. Output is
// one candidate per line (a tab separates an optional description), then a
// final ":<directive>" line in the cobra wire format.
func runCompleteDispatch(_ string, rest []string, stdout, _ io.Writer) int {
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	cfg, err := loadConfig(cli.Options{})
	if err != nil {
		// A broken user config must not break completion; fall back to the
		// built-in profiles.
		cfg = config.DefaultConfig()
	}
	candidates, directive := cli.Complete(cfg, commandSpecs(), rest)
	for _, candidate := range candidates {
		fmt.Fprintln(stdout, candidate)
	}
	fmt.Fprintf(stdout, ":%d\n", directive)
	return exitcode.OK
}
