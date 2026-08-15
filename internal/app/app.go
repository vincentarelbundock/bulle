package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"github.com/vincentarelbundock/bulle/internal/backends"
	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	"github.com/vincentarelbundock/bulle/internal/policy"
	"github.com/vincentarelbundock/bulle/internal/supervisor"
)

const (
	ExitOK               = 0
	ExitCommandFailed    = 1
	ExitConfigError      = 2
	ExitBackendMissing   = 3
	ExitPolicyValidation = 4
	ExitSandboxSetup     = 5
	ExitTimedOut         = 124
	ExitNotExecutable    = 126
	ExitNotFound         = 127
)

// Version is the bulle version, overridable at build time via
// -ldflags "-X github.com/vincentarelbundock/bulle/internal/app.Version=...".
var Version = "dev"

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if isPreparedPolicyRunner(args) {
		return runPreparedPolicy(args, stderr)
	}
	// An incomplete internal-runner invocation must not fall through to the
	// public CLI, where command inference would try to execute the reserved
	// name as a sandboxed command.
	if len(args) > 1 && args[1] == preparedPolicyRunnerCommand {
		fmt.Fprintf(stderr, "bulle: %s is an internal invocation and cannot be used directly\n", preparedPolicyRunnerCommand)
		return ExitConfigError
	}

	// Subcommands are dispatched by the first argument, before run parsing,
	// so command inference never tries to execute a verb in a sandbox. Any
	// other first argument is a run: the wrapper invocation stays bare.
	if len(args) > 1 {
		switch args[1] {
		case "scratch":
			// `bulle scratch` covers the whole lifecycle: the review verbs
			// resume a kept scratch, and anything else creates one, so the
			// subcommand is not limited to scratches that already exist.
			isRun, err := scratchArgsStartRun(args[2:])
			if err != nil {
				fmt.Fprintf(stderr, "bulle: %v\n", err)
				return ExitConfigError
			}
			if isRun {
				runArgs := append([]string{args[0], "--scratch"}, args[2:]...)
				return runMain(runArgs, "", stdout, stderr)
			}
			return runScratchCommand(args[2:], stdout, stderr)
		case "profiles":
			return runProfilesCommand(args[2:], stdout, stderr)
		case "resolvers":
			if len(args) > 2 {
				fmt.Fprintln(stderr, "usage: bulle resolvers")
				return ExitConfigError
			}
			writeResolverListing(parentEnv(), stdout)
			return ExitOK
		case "policy":
			runArgs, format, err := extractPolicyFormat(args[2:])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return ExitConfigError
			}
			return runMain(append([]string{args[0]}, runArgs...), format, stdout, stderr)
		case "rerun":
			stored, err := loadLastRun()
			if err != nil {
				fmt.Fprintln(stderr, "bulle: rerun: no previous invocation recorded")
				return ExitConfigError
			}
			merged := mergeLastRunArgs(stored.Args, args[2:])
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
			return runMain(append([]string{args[0]}, merged...), "", stdout, stderr)
		}
	}
	return runMain(args, "", stdout, stderr)
}

// extractPolicyFormat pulls --json out of `bulle policy` arguments; the rest
// are ordinary run arguments.
func extractPolicyFormat(args []string) ([]string, string, error) {
	format := "summary"
	out := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json":
			format = "json"
		case "--summary":
			format = "summary"
		default:
			out = append(out, arg)
		}
	}
	return out, format, nil
}

// runMain is the sandboxed run itself — and, when policyFormat is non-empty,
// the `bulle policy` variant that resolves and prints without running.
func runMain(args []string, policyFormat string, stdout io.Writer, stderr io.Writer) int {
	opts, err := cli.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	opts.Policy = policyFormat != ""
	opts.PolicyFormat = policyFormat
	if opts.Help {
		fmt.Fprint(stdout, cli.Usage())
		return ExitOK
	}
	if opts.Version {
		fmt.Fprintf(stdout, "bulle %s\n", Version)
		return ExitOK
	}
	for _, note := range opts.Notes {
		fmt.Fprintln(stderr, note)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	tmp := runtimeTempRoot(os.TempDir())
	if err := ensureRuntimeDirs(tmp); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	global, err := loadConfig(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	if !opts.NoDefaults {
		if err := applyConfigDefaults(&opts, global.Defaults); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitConfigError
		}
	}
	explicitCommand := len(opts.Command) > 0
	if len(opts.Command) == 0 {
		defaultApp, err := defaultAppForRun(opts, global)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitConfigError
		}
		if defaultApp != "" {
			command, err := shlex.Split(defaultApp)
			if err != nil {
				fmt.Fprintf(stderr, "invalid default_app: %v\n", err)
				return ExitConfigError
			}
			opts.Command = command
		}
	}
	// --policy resolves and prints without running anything, so it works
	// without a command; command-dependent grants (add_exec, shebang
	// discovery) are simply absent from the printed policy.
	if len(opts.Command) == 0 && !opts.Policy {
		fmt.Fprintln(stderr, "bulle: no command supplied and no default_app configured")
		fmt.Fprintln(stderr, "pass a command after -- (e.g. bulle . -- claude) or set default_app in your config")
		return ExitConfigError
	}
	// The scratch is created before policy.Resolve so $WORKSPACE and the
	// automatic read-write grant follow it; the origin path is never granted.
	// Scratches are deliberately absent from the deferred cleanup below: only
	// an empty change set or an explicit discard removes one.
	var scratch *scratchState
	if opts.Scratch {
		if opts.NoWorkspace {
			fmt.Fprintln(stderr, "bulle: --scratch with --no-workspace is contradictory: a scratch exists to be the workspace")
			return ExitConfigError
		}
		scratch, err = createScratch(opts.ProjectPath, global.Scratch.Dir, cli.NormalizeSeparator(args[1:]), stderr)
		if err != nil {
			fmt.Fprintf(stderr, "bulle: %v\n", err)
			return ExitConfigError
		}
		opts.ProjectPath = scratch.Dir
	}
	// Shim directories created for which:/pkg: entries are per-run; remove
	// them on the way out (normal exit, command failure, and timeout all
	// return through here).
	shimDirs := []string{}
	defer func() {
		for _, dir := range shimDirs {
			os.RemoveAll(dir)
		}
	}()
	env := parentEnv()
	p, err := policy.Resolve(policy.Inputs{Options: opts, Global: global, ParentEnv: env, Home: home, Tmp: tmp})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitPolicyValidation
	}
	if p.ShimDir != "" {
		shimDirs = append(shimDirs, p.ShimDir)
	}
	if _, err := backends.ForName(p.Backend); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitBackendMissing
	}
	prepared, err := backends.PreparePolicy(p)
	// Rescue-only profile inference: when the user gave a command but no
	// profile and discovery fails under the default profile, a profile whose
	// default_app runs that same command is almost certainly what they meant.
	// Runs that would succeed are never affected, and ambiguity refuses to
	// guess because selecting a profile changes what the sandbox grants.
	var ambiguousProfiles []string
	if err != nil && opts.Profile == "" && explicitCommand {
		matches := profilesMatchingCommand(global, opts.Command[0])
		if len(matches) == 1 {
			retry := opts
			retry.Profile = matches[0]
			if rescued, rerr := resolveAndPrepare(retry, global, env, home, tmp); rerr == nil {
				if rescued.ShimDir != "" {
					shimDirs = append(shimDirs, rescued.ShimDir)
				}
				fmt.Fprintf(stderr, "bulle: selected profile %q because its default_app runs %q and the default profile cannot; pass --profile to choose explicitly\n", matches[0], opts.Command[0])
				opts.Profile = matches[0]
				prepared, err = rescued, nil
			}
		} else if len(matches) > 1 {
			ambiguousProfiles = matches
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		if len(ambiguousProfiles) > 0 {
			fmt.Fprintf(stderr, "profiles %s all declare a default_app running %q; choose one with --profile\n", strings.Join(ambiguousProfiles, ", "), opts.Command[0])
		}
		if scratch != nil {
			// Nothing ran, so the scratch is unchanged; remove it rather than
			// leaving an empty clone behind on every failed policy resolution.
			removeScratch(scratch)
		}
		if errors.Is(err, policy.ErrCommandNotFound) {
			return ExitNotFound
		}
		return ExitNotExecutable
	}
	p = prepared
	if opts.Policy {
		if scratch != nil {
			// Nothing ran, so the scratch is empty by construction; remove it
			// after explaining the redirection.
			defer removeScratch(scratch)
			fmt.Fprintf(stdout, "scratch workspace: %s (origin: %s)\n", scratch.Dir, scratch.Origin)
		}
		switch opts.PolicyFormat {
		case "", "summary":
			writeProfilePermissionSummary(policySummaryProfileName(opts), p, stdout, true)
			writeResolutionTable(p, stdout)
		case "json":
			if err := json.NewEncoder(stdout).Encode(policy.NewView(p)); err != nil {
				fmt.Fprintln(stderr, err)
				return ExitCommandFailed
			}
		default:
			fmt.Fprintf(stderr, "invalid --policy value %q; use summary or json\n", opts.PolicyFormat)
			return ExitConfigError
		}
		return ExitOK
	}
	if code, ok := reportUnenforcedLimits(p, opts.Flags.StrictLimits, stderr); !ok {
		if scratch != nil {
			// Nothing ran, so the scratch is unchanged; remove it rather than
			// leaving an empty clone behind.
			removeScratch(scratch)
		}
		return code
	}
	saveLastRun(cli.NormalizeSeparator(args[1:]))
	for _, note := range p.Notes {
		fmt.Fprintf(stderr, "bulle: %s\n", note)
	}
	p.Command = commandWithSessionPermissions(opts.Profile, p.Command, preRunSessionPaste(opts, p))
	// All runs go through the supervisor (even without a timeout) so a parent
	// process survives the sandboxed command and can report on its failure.
	probe := startDenialProbe(p)
	code := ExitOK
	if err := supervisor.Run(p, supervisor.Options{
		Timeout: p.Timeout,
		Limits:  p.Limits,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}); err != nil {
		code = exitCodeForSupervisorError(err, stderr)
		printDenialHints(probe, scratch, home, stderr)
	}
	// The review gate runs on every exit path — success, command failure,
	// and timeout alike — and never changes the exit code.
	if scratch != nil {
		reviewScratch(scratch, opts.ScratchKeep, stdout, stderr)
	}
	return code
}

// printDenialHints reports sandbox denials logged by the kernel during a
// failed run, with copy-pasteable policy fixes. Best-effort: it prints
// nothing when denial logging is unsupported or kernel logs are unreadable.
func printDenialHints(probe denialProbe, scratch *scratchState, home string, stderr io.Writer) {
	hints := rewriteScratchPaths(probe.hints(), scratch, home)
	if len(hints) == 0 {
		return
	}
	const maxHints = 10
	fmt.Fprintln(stderr, "bulle: the sandbox denied the following accesses during this run:")
	for i, hint := range hints {
		if i == maxHints {
			fmt.Fprintf(stderr, "  ... and %d more (see journalctl --kernel)\n", len(hints)-maxHints)
			break
		}
		fmt.Fprintf(stderr, "  %s\n", hint)
	}
	if retry := retryHintLine(hints); retry != "" {
		fmt.Fprintln(stderr, retry)
	}
}

func resolveAndPrepare(opts cli.Options, global config.Config, env map[string]string, home string, tmp string) (policy.Policy, error) {
	p, err := policy.Resolve(policy.Inputs{Options: opts, Global: global, ParentEnv: env, Home: home, Tmp: tmp})
	if err != nil {
		return policy.Policy{}, err
	}
	if _, err := backends.ForName(p.Backend); err != nil {
		removeShimDir(p)
		return policy.Policy{}, err
	}
	prepared, err := backends.PreparePolicy(p)
	if err != nil {
		removeShimDir(p)
	}
	return prepared, err
}

func removeShimDir(p policy.Policy) {
	if p.ShimDir != "" {
		os.RemoveAll(p.ShimDir)
	}
}

func loadConfig(opts cli.Options) (config.Config, error) {
	global, err := config.LoadDefaultConfig()
	if err != nil {
		return config.Config{}, err
	}
	root := opts.Config
	explicit := root != ""
	if root == "" {
		root = defaultConfigRoot()
	}
	if root == "" {
		return global, nil
	}
	// Machine-local settings (notably [vars]) live in <root>/config.toml so a
	// portable profile can reference layouts this machine declares.
	if loaded, err := config.LoadFile(filepath.Join(root, "config.toml")); err == nil {
		global = config.MergeConfigs(global, loaded)
	} else if !os.IsNotExist(err) {
		return config.Config{}, err
	}
	loaded, err := config.LoadProfileDirectory(filepath.Join(root, "profiles"))
	if err == nil {
		global = config.MergeConfigs(global, loaded)
	} else if explicit || !os.IsNotExist(err) {
		return config.Config{}, err
	}
	return global, nil
}

func defaultAppForRun(opts cli.Options, global config.Config) (string, error) {
	profile, _, _, err := config.EffectiveProfile(global, opts.Profile)
	if err != nil {
		return "", err
	}
	return profile.DefaultApp, nil
}

func policySummaryProfileName(opts cli.Options) string {
	if opts.Profile != "" {
		return opts.Profile
	}
	return "default"
}

func defaultConfigRoot() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "bulle")
}

func parentEnv() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func isCommandExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func exitCodeForSupervisorError(err error, stderr io.Writer) int {
	var restoreErr *supervisor.TerminalRestoreError
	hasRestoreErr := errors.As(err, &restoreErr)
	var timeoutErr *supervisor.TimeoutError
	if errors.As(err, &timeoutErr) {
		fmt.Fprintf(stderr, "bulle: command timed out after %s\n", timeoutErr.Duration)
		if hasRestoreErr {
			fmt.Fprintln(stderr, restoreErr)
		}
		return ExitTimedOut
	}
	if hasRestoreErr {
		fmt.Fprintln(stderr, restoreErr)
		return ExitSandboxSetup
	}
	var exitErr *supervisor.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Code > 0 {
			return exitErr.Code
		}
		return ExitCommandFailed
	}
	fmt.Fprintln(stderr, err)
	return ExitSandboxSetup
}

func runtimeTempRoot(base string) string {
	return filepath.Join(base, "bulle-"+strconv.Itoa(os.Getuid()))
}

func ensureRuntimeDirs(tmp string) error {
	if err := ensurePrivateDir(tmp); err != nil {
		return err
	}
	root := filepath.Join(tmp, "bulle")
	for _, dir := range []string{root, filepath.Join(root, "tmp")} {
		if err := ensurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked runtime directory: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime path is not a directory: %s", path)
	}
	return os.Chmod(path, 0o700)
}
