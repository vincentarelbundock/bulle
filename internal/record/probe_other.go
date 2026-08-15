//go:build !linux

package record

import (
	"io"

	"github.com/vincentarelbundock/bulle/internal/exitcode"
)

// The denial-logging probe is Landlock-specific; other platforms never reach
// it because Supported refuses first.
func IsDenialLoggingProbe([]string) bool { return false }

func RunDenialLoggingProbe([]string, io.Writer) int { return exitcode.ConfigError }
