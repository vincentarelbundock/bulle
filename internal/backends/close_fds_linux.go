//go:build linux

package backends

import (
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func closeUnexpectedFileDescriptors() error {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		// /proc may be unavailable in unusual Linux environments. Prefer a single
		// close_range(2) covering every descriptor above stderr; fall back to a
		// bounded per-fd sweep only if the kernel lacks the syscall (pre-5.9).
		if rangeErr := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); rangeErr == nil {
			return nil
		}
		max := fallbackFDCeiling()
		for fd := 3; fd < max; fd++ {
			syscall.CloseOnExec(fd)
		}
		return nil
	}
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd <= 2 {
			continue
		}
		syscall.CloseOnExec(fd)
	}
	return nil
}
