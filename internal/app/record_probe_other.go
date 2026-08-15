//go:build !linux

package app

import (
	"io"

	"github.com/vincentarelbundock/bulle/internal/exitcode"
)

// The denial-logging probe is Landlock-specific; other platforms never reach
// it because recordingSupported refuses first.
func isDenialLoggingProbe([]string) bool { return false }

func runDenialLoggingProbe([]string, io.Writer) int { return exitcode.ConfigError }
