//go:build linux

package backends

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestOfflinePolicyDeniesIoUringNetworkSurface(t *testing.T) {
	denied := map[uintptr]bool{}
	for _, nr := range deniedNetworkSyscalls() {
		denied[nr] = true
	}
	for name, nr := range map[string]uintptr{
		"io_uring_setup":    unix.SYS_IO_URING_SETUP,
		"io_uring_enter":    unix.SYS_IO_URING_ENTER,
		"io_uring_register": unix.SYS_IO_URING_REGISTER,
	} {
		if !denied[nr] {
			t.Errorf("offline seccomp policy permits %s", name)
		}
	}
}
