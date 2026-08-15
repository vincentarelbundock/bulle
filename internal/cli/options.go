package cli

import (
	"time"

	"github.com/vincentarelbundock/bulle/internal/limits"
)

type Options struct {
	Flags

	// Profile is the first positional: a profile name, or comma-separated
	// profiles merged left to right.
	Profile     string
	ProjectPath string

	// AddExec and AddLibs are no longer flags: the app turns them on whenever
	// the command was given explicitly after --, so an arbitrary command works
	// without its binary being granted by hand.
	AddExec bool
	AddLibs bool
	Command []string
	Timeout time.Duration

	// Limits is the parsed resource-limit request, after the flags and the
	// [defaults] block have been merged.
	Limits limits.Limits

	// Policy and PolicyFormat are set by the `bulle policy` subcommand, not
	// by flags: resolve and print the policy instead of running.
	Policy       bool
	PolicyFormat string

	// Notes are informational messages produced during parsing (for example
	// when the command separator -- was inferred) for the app to print on
	// stderr.
	Notes []string
}
