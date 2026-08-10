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

	opts, err := cli.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	if opts.Help {
		fmt.Fprint(stdout, cli.Usage())
		return ExitOK
	}
	if opts.Version {
		fmt.Fprintf(stdout, "bulle %s\n", Version)
		return ExitOK
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
	if opts.InstallProfiles != "" {
		root := opts.Config
		if root == "" {
			root = defaultConfigRoot()
		}
		if root == "" {
			fmt.Fprintln(stderr, "could not determine user config directory")
			return ExitConfigError
		}
		if err := installProfiles(opts.InstallProfiles, root, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitConfigError
		}
		return ExitOK
	}
	global, err := loadConfig(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	if opts.ListProfiles {
		for _, name := range cli.ProfileNames(global) {
			fmt.Fprintln(stdout, name)
		}
		return ExitOK
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
	if len(opts.Command) == 0 {
		fmt.Fprintln(stderr, "bulle: no command supplied and no default_app configured")
		fmt.Fprintln(stderr, "pass a command after -- (e.g. bulle . -- claude) or set default_app in your config")
		return ExitConfigError
	}
	p, err := policy.Resolve(policy.Inputs{Options: opts, Global: global, ParentEnv: parentEnv(), Home: home, Tmp: tmp})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitPolicyValidation
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
			if rescued, rerr := resolveAndPrepare(retry, global, home, tmp); rerr == nil {
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
		if errors.Is(err, policy.ErrCommandNotFound) {
			return ExitNotFound
		}
		return ExitNotExecutable
	}
	p = prepared
	if opts.Policy {
		switch opts.PolicyFormat {
		case "", "summary":
			writeProfilePermissionSummary(policySummaryProfileName(opts), p, stdout)
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
	p.Command = commandWithSessionPermissions(opts.Profile, p.Command, preRunSessionPaste(opts, p))
	// All runs go through the supervisor (even without a timeout) so a parent
	// process survives the sandboxed command and can report on its failure.
	probe := startDenialProbe(p)
	if err := supervisor.Run(p, supervisor.Options{
		Timeout: p.Timeout,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}); err != nil {
		code := exitCodeForSupervisorError(err, stderr)
		printDenialHints(probe, stderr)
		return code
	}
	return ExitOK
}

// printDenialHints reports sandbox denials logged by the kernel during a
// failed run, with copy-pasteable policy fixes. Best-effort: it prints
// nothing when denial logging is unsupported or kernel logs are unreadable.
func printDenialHints(probe denialProbe, stderr io.Writer) {
	hints := probe.hints()
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

func resolveAndPrepare(opts cli.Options, global config.Config, home string, tmp string) (policy.Policy, error) {
	p, err := policy.Resolve(policy.Inputs{Options: opts, Global: global, ParentEnv: parentEnv(), Home: home, Tmp: tmp})
	if err != nil {
		return policy.Policy{}, err
	}
	if _, err := backends.ForName(p.Backend); err != nil {
		return policy.Policy{}, err
	}
	return backends.PreparePolicy(p)
}

func loadConfig(opts cli.Options) (config.Config, error) {
	global, err := config.LoadDefaultConfig()
	if err != nil {
		return config.Config{}, err
	}
	if opts.Config != "" {
		loaded, err := config.LoadProfileDirectory(filepath.Join(opts.Config, "profiles"))
		if err != nil {
			return config.Config{}, err
		}
		global = config.MergeConfigs(global, loaded)
	} else if root := defaultConfigRoot(); root != "" {
		if loaded, err := config.LoadProfileDirectory(filepath.Join(root, "profiles")); err == nil {
			global = config.MergeConfigs(global, loaded)
		} else if !os.IsNotExist(err) {
			return config.Config{}, err
		}
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
