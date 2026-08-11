package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

// lastRun records the most recent real invocation so `bulle --last` can
// repeat it from a fresh shell, optionally with extra grants appended.
type lastRun struct {
	Args []string `json:"args"`
	Dir  string   `json:"cwd"`
}

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

func lastRunPath() string {
	root := stateRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "bulle", "last-run.json")
}

// saveLastRun persists the invocation best-effort: a failure to record the
// last run must never fail the run itself.
func saveLastRun(args []string) {
	path := lastRunPath()
	if path == "" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	data, err := json.Marshal(lastRun{Args: args, Dir: cwd})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func loadLastRun() (lastRun, error) {
	path := lastRunPath()
	if path == "" {
		return lastRun{}, fmt.Errorf("could not determine the state directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return lastRun{}, err
	}
	var last lastRun
	if err := json.Unmarshal(data, &last); err != nil {
		return lastRun{}, err
	}
	return last, nil
}

// mergeLastRunArgs combines the stored invocation with the current one: new
// arguments (minus the --last flag itself) are inserted before the stored
// command's "--" separator, so added grants stay bulle flags instead of
// leaking into the sandboxed command.
func mergeLastRunArgs(stored []string, current []string) []string {
	extra := []string{}
	for _, arg := range current {
		if arg == "--last" {
			continue
		}
		extra = append(extra, arg)
	}
	for i, arg := range stored {
		if arg == "--" {
			merged := append([]string{}, stored[:i]...)
			merged = append(merged, extra...)
			return append(merged, stored[i:]...)
		}
	}
	return append(append([]string{}, stored...), extra...)
}

// retryHintLine builds the copy-pasteable rerun suggestion from denial
// hints of the form "denied: VERB PATH — add --ro PATH".
func retryHintLine(hints []string) string {
	grants := []string{}
	seen := map[string]bool{}
	for _, hint := range hints {
		_, grant, ok := strings.Cut(hint, " — add ")
		if !ok || seen[grant] {
			continue
		}
		seen[grant] = true
		grants = append(grants, grant)
	}
	if len(grants) == 0 {
		return ""
	}
	const maxGrants = 10
	if len(grants) > maxGrants {
		grants = grants[:maxGrants]
	}
	return "bulle: retry with these grants: bulle rerun " + strings.Join(grants, " ")
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
