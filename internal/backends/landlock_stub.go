//go:build !linux

package backends

import (
	"fmt"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

type landlockBackend struct{}

func newLandlockBackend() Backend { return landlockBackend{} }

func (landlockBackend) Run(policy.Policy) error {
	return fmt.Errorf("linux-landlock backend is only available on Linux")
}
