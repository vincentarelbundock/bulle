package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/shlex"
	"github.com/vincentarelbundock/bulle/internal/backends"
	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	"github.com/vincentarelbundock/bulle/internal/didyoumean"
	benv "github.com/vincentarelbundock/bulle/internal/env"
	"github.com/vincentarelbundock/bulle/internal/exitcode"
	"github.com/vincentarelbundock/bulle/internal/limits"
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
	"github.com/vincentarelbundock/bulle/internal/record"
	"github.com/vincentarelbundock/bulle/internal/supervisor"
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
		return exitcode.ConfigError
	}

	// Subcommands are dispatched by the first argument, before run parsing,
	// so command inference never tries to execute a verb in a sandbox. Any
	// other first argument is a run: the wrapper invocation stays bare.
	// Dispatch reads the subcommand table in commands.go — the same table
	// shell completion answers from.
	if len(args) > 1 {
		for _, sub := range subcommands() {
			if sub.Name == args[1] {
				rest := args[2:]
				// --help before the -- separator, or a bare invocation of a
				// subcommand that needs arguments, prints that subcommand's
				// help instead of a terse usage error.
				if wantsHelp(rest) || (len(rest) == 0 && sub.helpWhenBare) {
					return printCommandHelp(sub.Name, stdout)
				}
				return sub.run(args[0], rest, stdout, stderr)
			}
		}
	}
	return runMain(args, "", stdout, stderr, record.NewRecorder())
}

// extractPolicyFormat pulls --json out of `bulle policy` arguments; the rest
// are ordinary run arguments. Everything from the first -- onwards belongs to
// the command being described and is passed through untouched: `curl --json`
// and `gh --json` are real invocations, and stealing that flag would both flip
// bulle's output format and report a command line other than the one that
// would run.
func extractPolicyFormat(args []string) ([]string, string, error) {
	format := "summary"
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
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
func runMain(args []string, policyFormat string, stdout io.Writer, stderr io.Writer, rec *record.Recorder) int {
	opts, err := cli.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.ConfigError
	}
	opts.Policy = policyFormat != ""
	opts.PolicyFormat = policyFormat
	if opts.Help {
		fmt.Fprint(stdout, cli.Usage())
		return exitcode.OK
	}
	if opts.Version {
		fmt.Fprintf(stdout, "bulle %s\n", Version)
		return exitcode.OK
	}
	for _, note := range opts.Notes {
		fmt.Fprintln(stderr, note)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.ConfigError
	}
	tmp := bpaths.RuntimeTempRoot(os.TempDir())
	if err := ensureRuntimeDirs(tmp); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.ConfigError
	}
	global, err := loadConfig(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.ConfigError
	}
	if !opts.NoDefaults {
		if err := applyConfigDefaults(&opts, global.Defaults); err != nil {
			fmt.Fprintln(stderr, err)
			return exitcode.ConfigError
		}
	}
	if err := validateProfiles(opts, global); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.ConfigError
	}
	explicitCommand := len(opts.Command) > 0
	if explicitCommand {
		// A command given explicitly after -- always gets its own binary
		// granted: `bulle -- pandoc doc.md` must work without the binary
		// being granted by hand.
		opts.AddExec = true
		// Library discovery scans the granted executable trees, which a
		// profile can make enormous; with no profile selected the sandbox is
		// minimal and the scan is what makes an arbitrary command runnable.
		opts.AddLibs = opts.Profile == ""
	}
	if len(opts.Command) == 0 {
		defaultApp, err := defaultAppForRun(opts, global)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitcode.ConfigError
		}
		if defaultApp != "" {
			command, err := shlex.Split(defaultApp)
			if err != nil {
				fmt.Fprintf(stderr, "invalid default_app: %v\n", err)
				return exitcode.ConfigError
			}
			opts.Command = command
		}
	}
	// --policy resolves and prints without running anything, so it works
	// without a command; command-dependent grants (add_exec, shebang
	// discovery) are simply absent from the printed policy.
	if len(opts.Command) == 0 && !opts.Policy {
		// A completely bare `bulle` with nothing configured to run reads as
		// a request for orientation, not a broken invocation.
		if len(args) == 1 {
			fmt.Fprint(stdout, cli.Usage())
			return exitcode.OK
		}
		if opts.Profile != "" {
			fmt.Fprintf(stderr, "bulle: profile %q has no default app; add a command after -- (e.g. bulle %s -- ./script.sh)\n", opts.Profile, opts.Profile)
		} else {
			fmt.Fprintln(stderr, "bulle: nothing to run: name a profile with a default app, or pass a command after --")
		}
		return exitcode.ConfigError
	}
	// The scratch is created before policy.Resolve so $WORKSPACE and the
	// automatic read-write grant follow it; the origin path is never granted.
	// Scratches are deliberately absent from the deferred cleanup below: only
	// an empty change set or an explicit discard removes one.
	var scratch *scratchState
	if opts.Scratch {
		if opts.NoWorkspace {
			fmt.Fprintln(stderr, "bulle: --scratch with --no-workspace is contradictory: a scratch exists to be the workspace")
			return exitcode.ConfigError
		}
		scratch, err = createScratch(opts.ProjectPath, global.Scratch.Dir, cli.NormalizeSeparator(args[1:]), stderr)
		if err != nil {
			fmt.Fprintf(stderr, "bulle: %v\n", err)
			return exitcode.ConfigError
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
	env := benv.Parent()
	p, err := policy.Resolve(policy.Inputs{Options: opts, Global: global, ParentEnv: env, Home: home, Tmp: tmp})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.PolicyValidation
	}
	if p.ShimDir != "" {
		shimDirs = append(shimDirs, p.ShimDir)
	}
	if _, err := backends.ForName(p.Backend); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.BackendMissing
	}
	prepared, err := backends.PreparePolicy(p)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if scratch != nil {
			// Nothing ran, so the scratch is unchanged; remove it rather than
			// leaving an empty clone behind on every failed policy resolution.
			removeScratch(scratch)
		}
		if errors.Is(err, policy.ErrCommandNotFound) {
			return exitcode.NotFound
		}
		return exitcode.NotExecutable
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
				return exitcode.CommandFailed
			}
		default:
			fmt.Fprintf(stderr, "invalid --policy value %q; use summary or json\n", opts.PolicyFormat)
			return exitcode.ConfigError
		}
		return exitcode.OK
	}
	if code, ok := reportUnenforcedLimits(p, opts.Flags.StrictLimits, stderr); !ok {
		if scratch != nil {
			// Nothing ran, so the scratch is unchanged; remove it rather than
			// leaving an empty clone behind.
			removeScratch(scratch)
		}
		return code
	}
	for _, note := range p.Notes {
		fmt.Fprintf(stderr, "bulle: %s\n", note)
	}
	// All runs go through the supervisor (even without a timeout) so a parent
	// process survives the sandboxed command and can report on its failure.
	probe := record.StartProbe(&p)
	defer probe.Close()
	code := exitcode.OK
	failed := false
	if err := supervisor.Run(p, supervisor.Options{
		Timeout:         p.Timeout,
		Limits:          p.Limits,
		CgroupSupported: limits.Current().Cgroup,
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
	}); err != nil {
		code = exitCodeForSupervisorError(err, stderr)
		failed = true
	}
	// The recorder reads the same denials the hints are built from, against
	// the policy that was actually in effect for this run. It observes
	// successes too: a run can be denied an optional access and still exit
	// zero, and that grant belongs in the profile.
	if rec != nil {
		rec.BeginRound()
		rec.Observe(p, probe)
		if rec.LastAdded > 0 {
			record.ReportLearnedGrants(opts, global, rec, scratchRewrite(scratch), stderr)
		} else if failed {
			printDenialHints(probe, scratch, home, stderr)
		}
	} else if failed {
		printDenialHints(probe, scratch, home, stderr)
	}
	// The review gate runs on every exit path — success, command failure,
	// and timeout alike — and never changes the exit code.
	if scratch != nil {
		reviewScratch(scratch, opts.ScratchKeep, stdout, stderr)
	}
	return code
}

// validateProfiles rejects unknown profile names before anything runs, with a
// suggestion for a near miss and a pointer at the two likeliest grammar slips:
// a directory in the profile slot, and a command name without the -- that a
// command requires.
func validateProfiles(opts cli.Options, global config.Config) error {
	if opts.Profile == "" {
		return nil
	}
	for _, part := range strings.Split(opts.Profile, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return fmt.Errorf("bulle: profile list contains an empty name")
		}
		if _, ok := global.Profiles[name]; ok {
			continue
		}
		if info, err := os.Stat(name); err == nil && info.IsDir() {
			return fmt.Errorf("bulle: %q is a directory, not a profile; the workspace comes second, as in: bulle default %s", name, name)
		}
		msg := fmt.Sprintf("bulle: unknown profile %q", name)
		if suggestion := didyoumean.Closest(name, profileNameList(global)); suggestion != "" {
			msg += fmt.Sprintf(" (did you mean %s?)", suggestion)
		}
		if _, err := exec.LookPath(name); err == nil {
			msg += fmt.Sprintf("; to run the command %q, put it after the separator: bulle -- %s", name, name)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func profileNameList(global config.Config) []string {
	names := make([]string, 0, len(global.Profiles))
	for name := range global.Profiles {
		names = append(names, name)
	}
	return names
}

// printDenialHints reports sandbox denials logged by the kernel during a
// failed run, with copy-pasteable policy fixes. Best-effort: it prints
// nothing when denial logging is unsupported or kernel logs are unreadable.
func printDenialHints(probe record.Probe, scratch *scratchState, home string, stderr io.Writer) {
	hints := rewriteScratchPaths(probe.Hints(), scratch, home)
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
		root = config.DefaultRoot()
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
		return exitcode.TimedOut
	}
	if hasRestoreErr {
		fmt.Fprintln(stderr, restoreErr)
		return exitcode.SandboxSetup
	}
	var exitErr *supervisor.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Code > 0 {
			return exitErr.Code
		}
		return exitcode.CommandFailed
	}
	fmt.Fprintln(stderr, err)
	return exitcode.SandboxSetup
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
