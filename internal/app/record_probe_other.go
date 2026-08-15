//go:build !linux

package app

import "io"

// The denial-logging probe is Landlock-specific; other platforms never reach
// it because recordingSupported refuses first.
func isDenialLoggingProbe([]string) bool { return false }

func runDenialLoggingProbe([]string, io.Writer) int { return ExitConfigError }
