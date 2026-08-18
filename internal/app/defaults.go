package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	"github.com/vincentarelbundock/bulle/internal/limits"
)

// stateRoot is the per-user state directory bulle keeps run artifacts under
// (notably scratch bookkeeping).
func stateRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support")
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" && filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(home, ".local", "state")
}

// applyConfigDefaults fills flag-shaped gaps from the [defaults] block of the
// user configuration. Explicit flags always win; list-valued defaults are
// prepended so command-line entries stay the most explicit.
func applyConfigDefaults(opts *cli.Options, defaults config.DefaultsSettings) error {
	if opts.Profile == "" {
		opts.Profile = defaults.Profile
	}
	if opts.Flags.Timeout == "" && defaults.Timeout != "" && defaults.Timeout != "0" {
		duration, err := time.ParseDuration(defaults.Timeout)
		if err != nil || duration < 0 {
			return fmt.Errorf("invalid [defaults] timeout %q; use a Go duration such as 30s, 2m, or 1h30m", defaults.Timeout)
		}
		opts.Timeout = duration
	}
	// The [defaults] limits sit underneath the flags, and the platform-scoped
	// block sits between them: a limit written only under [defaults.macos] is
	// simply not requested on Linux, and so is never reported as unenforced.
	resolved, err := limits.Parse(defaults.LimitSpec(runtime.GOOS).Merge(opts.Flags.LimitSpec()))
	if err != nil {
		// cli.Parse already validated the flags on their own, so a failure here
		// can only come from a value the configuration contributed. Say so:
		// the message names a flag, and the user needs to know where to look.
		return fmt.Errorf("%w (from the [defaults] limits of the user configuration)", err)
	}
	opts.Limits = resolved
	if !opts.Flags.StrictLimits && defaults.StrictLimits != nil {
		opts.Flags.StrictLimits = *defaults.StrictLimits
	}
	opts.Env = prependList(defaults.Env, opts.Env)
	opts.ReadOnly = prependList(defaults.ReadOnly, opts.ReadOnly)
	opts.ReadOnlyExec = prependList(defaults.ReadOnlyExec, opts.ReadOnlyExec)
	opts.ReadWrite = prependList(defaults.ReadWrite, opts.ReadWrite)
	opts.ReadWriteExec = prependList(defaults.ReadWriteExec, opts.ReadWriteExec)
	return nil
}

func prependList(defaults []string, explicit []string) []string {
	if len(defaults) == 0 {
		return explicit
	}
	return append(append([]string{}, defaults...), explicit...)
}
