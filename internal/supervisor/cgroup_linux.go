package supervisor

import (
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
// A failure to create the cgroup is not fatal: the caller has already reported
// the affected limits as unenforced, so the run proceeds unconstrained rather
// than failing on a machine that simply has no delegation.
func prepareCgroup(cmd *exec.Cmd, l limits.Limits) *runCgroup {
	if l.Memory == 0 && l.CPU == 0 && l.NProc == 0 {
		return nil
	}
	cgroup, err := limits.Create(l)
	if err != nil {
		return nil
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = cgroup.FD()
	return &runCgroup{cgroup: cgroup}
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
