package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
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
		{cli.CommandSpec{Name: "record", Extra: []cli.FlagSpec{
			{Name: "out", Short: "o", TakesValue: true, Complete: "file", Help: "Write the recorded profile to FILE instead of stdout."},
			{Name: "max-rounds", TakesValue: true, Help: "Stop after N recording rounds (default 10)."},
		}}, runRecordDispatch, true},
		{cli.CommandSpec{Name: "profiles", Verbs: []string{"list", "install"}}, runProfilesDispatch, true},
		{cli.CommandSpec{Name: "resolvers"}, runResolversDispatch, false},
		{cli.CommandSpec{Name: "policy", Extra: []cli.FlagSpec{
			{Name: "json", Help: "Print the resolved policy as JSON."},
			{Name: "summary", Help: "Print the resolved policy as a summary (default)."},
		}}, runPolicyDispatch, false},
		{cli.CommandSpec{Name: "rerun"}, runRerunDispatch, false},
		{cli.CommandSpec{Name: "completion", Verbs: []string{"bash", "zsh", "fish"}}, runCompletionDispatch, true},
		{cli.CommandSpec{Name: "help"}, runHelpDispatch, false},
		{cli.CommandSpec{Name: "version"}, runVersionDispatch, false},
		{cli.CommandSpec{Name: "__complete", Hidden: true}, runCompleteDispatch, false},
	}
	// `bulle help <topic>` completes the other subcommand names as verbs.
	for i := range subs {
		if subs[i].Name == "help" {
			for _, sub := range subs {
				if !sub.Hidden && sub.Name != "help" {
					subs[i].Verbs = append(subs[i].Verbs, sub.Name)
				}
			}
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
		return ExitOK
	}
	fmt.Fprint(stdout, cli.Usage())
	return ExitOK
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
		return ExitConfigError
	}
	if isRun {
		runArgs := append([]string{argv0, "--scratch"}, rest...)
		return runMain(runArgs, "", stdout, stderr, nil)
	}
	return runScratchCommand(rest, stdout, stderr)
}

func runRecordDispatch(argv0 string, rest []string, stdout, stderr io.Writer) int {
	return runRecordCommand(argv0, rest, stdout, stderr)
}

func runProfilesDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	return runProfilesCommand(rest, stdout, stderr)
}

func runResolversDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) > 0 {
		fmt.Fprintln(stderr, "usage: bulle resolvers")
		return ExitConfigError
	}
	writeResolverListing(parentEnv(), stdout)
	return ExitOK
}

func runPolicyDispatch(argv0 string, rest []string, stdout, stderr io.Writer) int {
	runArgs, format, err := extractPolicyFormat(rest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	return runMain(append([]string{argv0}, runArgs...), format, stdout, stderr, nil)
}

func runRerunDispatch(argv0 string, rest []string, stdout, stderr io.Writer) int {
	stored, err := loadLastRun()
	if err != nil {
		fmt.Fprintln(stderr, "bulle: rerun: no previous invocation recorded")
		return ExitConfigError
	}
	merged := mergeLastRunArgs(stored.Args, rest)
	if stored.Dir != "" {
		if cwd, err := os.Getwd(); err == nil && cwd != stored.Dir {
			if err := os.Chdir(stored.Dir); err != nil {
				fmt.Fprintf(stderr, "bulle: rerun: cannot return to %s: %v\n", stored.Dir, err)
				return ExitConfigError
			}
			fmt.Fprintf(stderr, "bulle: rerun: running from %s\n", stored.Dir)
		}
	}
	fmt.Fprintf(stderr, "bulle: repeating: bulle %s\n", strings.Join(merged, " "))
	return runMain(append([]string{argv0}, merged...), "", stdout, stderr, nil)
}

func runCompletionDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "usage: bulle completion bash|zsh|fish")
		return ExitConfigError
	}
	script, err := cli.CompletionScript(rest[0])
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		return ExitConfigError
	}
	fmt.Fprint(stdout, script)
	return ExitOK
}

func runHelpDispatch(_ string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) == 0 {
		fmt.Fprint(stdout, cli.Usage())
		return ExitOK
	}
	if text, ok := cli.CommandHelp(rest[0]); ok {
		fmt.Fprint(stdout, text)
		return ExitOK
	}
	var topics []string
	for _, sub := range subcommands() {
		if !sub.Hidden {
			topics = append(topics, sub.Name)
		}
	}
	fmt.Fprintf(stderr, "bulle: no help topic %q; topics: %s\n", rest[0], strings.Join(topics, ", "))
	return ExitConfigError
}

func runVersionDispatch(_ string, _ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "bulle %s\n", Version)
	return ExitOK
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
	return ExitOK
}
