//go:build !linux

package supervisor

import (
	"fmt"
	"os/exec"

	"github.com/vincentarelbundock/bulle/internal/limits"
)

// runCgroup has no implementation off Linux; the cgroup-backed limits are
// reported as unenforced before the run ever reaches here.
type runCgroup struct{}

func prepareCgroup(cmd *exec.Cmd, l limits.Limits, supported bool, requireTree bool) (*runCgroup, error) {
	if requireTree {
		return nil, fmt.Errorf("--timeout is unavailable on this platform because it cannot securely contain descendants that call setsid")
	}
	return nil, nil
}

func (c *runCgroup) kill() bool { return false }

func (c *runCgroup) close() {}
