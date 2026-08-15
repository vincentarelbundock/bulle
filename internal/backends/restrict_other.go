//go:build !linux

package backends

import (
	"fmt"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

// ApplyFilesystemRestrictions is Linux-only; see the Landlock implementation.
func ApplyFilesystemRestrictions(policy.Policy) error {
	return fmt.Errorf("filesystem restrictions can only be applied directly on Linux")
}
