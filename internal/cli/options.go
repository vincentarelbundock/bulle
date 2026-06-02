package cli

import "time"

type Options struct {
	Flags

	ProjectPath  string
	Command      []string
	PolicyFormat string
	Timeout      time.Duration
}
