//go:build linux

package backends

import (
	"os"
	"strconv"
	"syscall"
)

func closeUnexpectedFileDescriptors() error {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		// /proc may be unavailable in unusual Linux environments. Fall back to
		// the actual descriptor ceiling (RLIMIT_NOFILE) rather than a fixed
		// range, so an inherited fd above 1024 cannot survive into the sandbox.
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
// sweep used when /proc/self/fd is unreadable. It uses the soft RLIMIT_NOFILE
// so every potentially-open descriptor is covered, with a sane floor and an
// upper cap to bound the loop when the limit is effectively unlimited.
func fallbackFDCeiling() int {
	const floor = 1024
	const ceiling = 1 << 20
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return floor
	}
	max := int(lim.Cur)
	if int64(max) != int64(lim.Cur) || max > ceiling {
		return ceiling
	}
	if max < floor {
		return floor
	}
	return max
}
