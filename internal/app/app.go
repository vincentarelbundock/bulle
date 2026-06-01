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
)

const (
	ExitOK               = 0
	ExitCommandFailed    = 1
	ExitConfigError      = 2
	ExitBackendMissing   = 3
	ExitPolicyValidation = 4
	ExitSandboxSetup     = 5
	ExitNotExecutable    = 126
	ExitNotFound         = 127
)

// Version is the bulle version, overridable at build time via
// -ldflags "-X github.com/vincentarelbundock/bulle/internal/app.Version=...".
var Version = "dev"

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
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
	backend, err := backends.ForName(p.Backend)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitBackendMissing
	}
	prepared, err := backends.PreparePolicy(p)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, policy.ErrCommandNotFound) {
			return ExitNotFound
		}
		return ExitNotExecutable
	}
	p = prepared
	if opts.Policy {
		if err := json.NewEncoder(stdout).Encode(policy.NewView(p)); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitCommandFailed
		}
		return ExitOK
	}
	p.Command = commandWithSessionPermissions(opts.Profile, p.Command, preRunSessionPaste(opts, p))
	if err := backend.Run(p); err != nil {
		fmt.Fprintln(stderr, err)
		if isCommandExitError(err) {
			return ExitCommandFailed
		}
		return ExitSandboxSetup
	}
	return ExitOK
}

func loadConfig(opts cli.Options) (config.Config, error) {
	global, err := config.LoadDefaultConfig()
	if err != nil {
		return config.Config{}, err
	}
	if opts.Config != "" {
		loaded, err := config.LoadFile(opts.Config)
		if err != nil {
			return config.Config{}, err
		}
		global = config.MergeConfigs(global, loaded)
	} else if path := defaultConfigPath(); path != "" {
		if loaded, err := config.LoadFile(path); err == nil {
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

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "bulle", "config.toml")
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
