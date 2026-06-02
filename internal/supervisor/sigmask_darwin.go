//go:build darwin

package supervisor

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	darwinSIGBLOCK   = 1
	darwinSIGSETMASK = 3
)

func blockSIGTTOU(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var set uint32
	var old uint32
	set = 1 << (uint(syscall.SIGTTOU) - 1)

	if err := pthreadSigmask(darwinSIGBLOCK, &set, &old); err != nil {
		return err
	}
	err := fn()
	if restoreErr := pthreadSigmask(darwinSIGSETMASK, &old, nil); restoreErr != nil {
		return errors.Join(err, restoreErr)
	}
	return err
}

func pthreadSigmask(how int, set *uint32, old *uint32) error {
	var setPtr uintptr
	if set != nil {
		setPtr = uintptr(unsafe.Pointer(set))
	}
	var oldPtr uintptr
	if old != nil {
		oldPtr = uintptr(unsafe.Pointer(old))
	}
	_, _, errno := syscall.RawSyscall6(syscall.SYS___PTHREAD_SIGMASK, uintptr(how), setPtr, oldPtr, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
