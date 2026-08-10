package cli

import "time"

type Options struct {
	Flags

	ProjectPath  string
	Command      []string
	PolicyFormat string
	Timeout      time.Duration

	// Notes are informational messages produced during parsing (for example
	// when the command separator -- was inferred) for the app to print on
	// stderr.
	Notes []string
}
