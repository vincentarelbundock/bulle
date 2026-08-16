//go:build linux

package backends

import (
	"fmt"
	"os"
	"runtime"
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
	// The seccomp network filter is installed with PR_SET_SECCOMP, which is
	// per-thread: there is no TSYNC outside seccomp(2). Pin the goroutine to
	// its OS thread and keep it pinned through the exec, or the runtime is free
	// to resume it on a thread that never got the filter — and since exec tears
	// down every other thread, the command would then run with sockets fully
	// permitted while bulle reports the network as offline. Everything the exec
	// needs is computed before the filter goes on, so no allocation between the
	// two can turn into a scheduling point that matters.
	env := envSlice(p.Env)
	runtime.LockOSThread()
	if err := applyLinuxNetworkPolicy(p); err != nil {
		runtime.UnlockOSThread()
		return err
	}
	return syscall.Exec(p.Command[0], p.Command, env)
}
