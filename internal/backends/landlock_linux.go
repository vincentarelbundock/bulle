//go:build linux

package backends

import (
	"fmt"
	"os"
	"syscall"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

type landlockBackend struct{}

func newLandlockBackend() Backend { return landlockBackend{} }

func (landlockBackend) Run(p policy.Policy) error {
	if len(p.Command) == 0 {
		return fmt.Errorf("missing command")
	}
	if p.ProjectPath != "" {
		if err := os.Chdir(p.ProjectPath); err != nil {
			return err
		}
	}
	if err := closeUnexpectedFileDescriptors(); err != nil {
		return err
	}
	if err := applyLandlockFilesystem(p); err != nil {
		return err
	}
	return syscall.Exec(p.Command[0], p.Command, envSlice(p.Env))
}
