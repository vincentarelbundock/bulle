//go:build !darwin

package backends

import (
	"fmt"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

type seatbeltBackend struct{}

func newSeatbeltBackend() Backend { return seatbeltBackend{} }

func (seatbeltBackend) Run(policy.Policy) error {
	return fmt.Errorf("macos-seatbelt backend is only available on macOS")
}
