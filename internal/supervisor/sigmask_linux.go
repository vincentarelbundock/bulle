//go:build linux

package supervisor

import (
	"errors"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

func blockSIGTTOU(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var set unix.Sigset_t
	var old unix.Sigset_t
	set.Val[0] = 1 << (uint(syscall.SIGTTOU) - 1)

	if err := unix.PthreadSigmask(unix.SIG_BLOCK, &set, &old); err != nil {
		return err
	}
	err := fn()
	if restoreErr := unix.PthreadSigmask(unix.SIG_SETMASK, &old, nil); restoreErr != nil {
		return errors.Join(err, restoreErr)
	}
	return err
}
