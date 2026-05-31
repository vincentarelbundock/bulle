//go:build darwin

package backends

import (
	"os"
	"strconv"
	"syscall"
)

func closeUnexpectedFileDescriptors() error {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		for fd := 3; fd < 1024; fd++ {
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
