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

// fallbackFDCeiling returns the upper bound (exclusive) for the fixed-range fd
// sweep used when both /proc/self/fd and close_range(2) are unavailable. It uses
// the soft RLIMIT_NOFILE so every potentially-open descriptor is covered, with a
// sane floor and an upper cap to bound the loop when the limit is unlimited.
func fallbackFDCeiling() int {
	const floor = 1024
	const ceiling = 1 << 20
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return floor
	}
	// lim.Cur is unsigned; compare before any narrowing conversion so that
	// RLIM_INFINITY (math.MaxUint64) does not wrap to a small or negative int.
	if lim.Cur > uint64(ceiling) {
		return ceiling
	}
	if lim.Cur < uint64(floor) {
		return floor
	}
	return int(lim.Cur)
}
