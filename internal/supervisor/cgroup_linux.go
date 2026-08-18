package supervisor

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/vincentarelbundock/bulle/internal/limits"
)

// runCgroup is the cgroup holding the sandboxed process tree, or nil when no
// cgroup-backed limit was requested or none could be created.
type runCgroup struct {
	cgroup *limits.Cgroup
}

// prepareCgroup creates the run's cgroup and arranges for the child to be born
// inside it. Using clone3's cgroup argument rather than writing the pid into
// cgroup.procs after the fact removes the window in which the child could run
// — or fork — before the limits applied.
//
// A failure to create the cgroup is fatal when this machine was reported as
// able to enforce the limits, and silent when it was not. The distinction is
// the whole point: a run told the user "memory: 4G (cgroup v2)" and then
// proceeding without a cap is a limit that exists only on screen, while a
// machine with no delegation at all has already been warned about.
func prepareCgroup(cmd *exec.Cmd, l limits.Limits, supported bool, requireTree bool) (*runCgroup, error) {
	if l.Memory == 0 && l.CPU == 0 && l.NProc == 0 && !requireTree {
		return nil, nil
	}
	cgroup, err := limits.Create(l)
	if err != nil {
		if requireTree {
			return nil, fmt.Errorf("--timeout requires a delegated cgroup so descendants cannot escape with setsid: %w", err)
		}
		if supported {
			return nil, fmt.Errorf("the requested limits were reported as enforced, but the run's cgroup could not be created: %w", err)
		}
		return nil, nil
	}
	if requireTree {
		if err := cgroup.CanKill(); err != nil {
			_ = cgroup.Close()
			return nil, fmt.Errorf("--timeout requires atomic cgroup process-tree termination: %w", err)
		}
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = cgroup.FD()
	return &runCgroup{cgroup: cgroup}, nil
}

// kill terminates every process in the cgroup at once. It reports whether the
// kill was actually delivered, so the caller knows whether it still needs to
// fall back to signalling the process group.
func (c *runCgroup) kill() bool {
	if c == nil {
		return false
	}
	return c.cgroup.Kill() == nil
}

func (c *runCgroup) close() {
	if c == nil {
		return
	}
	_ = c.cgroup.Close()
}
