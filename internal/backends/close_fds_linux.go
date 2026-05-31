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
		// /proc may be unavailable in unusual Linux environments. Fall back to a
		// conservative fixed range rather than failing open.
		for fd := 3; fd < 1024; fd++ {
			_ = syscall.Close(fd)
		}
		return nil
	}
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd <= 2 {
			continue
		}
		_ = syscall.Close(fd)
	}
	return nil
}
