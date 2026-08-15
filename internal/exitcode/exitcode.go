// Package exitcode holds the process exit codes bulle reports.
//
// They live outside the command layer because more than one package decides an
// exit status — the CLI dispatcher, the sandboxed runner, and the recording
// probe — and a shared vocabulary keeps the numbers from being reinvented per
// package.
package exitcode

const (
	OK               = 0
	CommandFailed    = 1
	ConfigError      = 2
	BackendMissing   = 3
	PolicyValidation = 4
	SandboxSetup     = 5
	TimedOut         = 124
	NotExecutable    = 126
	NotFound         = 127
)
