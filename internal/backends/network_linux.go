//go:build linux

package backends

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

const (
	bpfLD  = 0x00
	bpfW   = 0x00
	bpfABS = 0x20
	bpfJMP = 0x05
	bpfJEQ = 0x10
	bpfK   = 0x00
	bpfRET = 0x06

	seccompModeFilter = 2
	seccompRetAllow   = 0x7fff0000
	seccompRetErrno   = 0x00050000
	seccompDataNR     = 0
)

func applyLinuxNetworkPolicy(p policy.Policy) error {
	if p.Network != policy.NetworkNone {
		return nil
	}
	if err := installDenySocketSeccompFilter(); err != nil {
		return fmt.Errorf("failed to apply Linux network restrictions: %w", err)
	}
	return nil
}

func installDenySocketSeccompFilter() error {
	filters := []unix.SockFilter{
		bpfStmt(bpfLD|bpfW|bpfABS, seccompDataNR),
	}
	for _, nr := range deniedNetworkSyscalls() {
		filters = append(filters,
			bpfJump(bpfJMP|bpfJEQ|bpfK, uint32(nr), 0, 1),
			bpfStmt(bpfRET|bpfK, seccompRetErrno|uint32(syscall.EPERM)),
		)
	}
	filters = append(filters, bpfStmt(bpfRET|bpfK, seccompRetAllow))
	program := unix.SockFprog{
		Len:    uint16(len(filters)),
		Filter: &filters[0],
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	return unix.Prctl(unix.PR_SET_SECCOMP, seccompModeFilter, uintptr(unsafe.Pointer(&program)), 0, 0)
}

func deniedNetworkSyscalls() []uintptr {
	return []uintptr{
		unix.SYS_SOCKET,
		unix.SYS_SOCKETPAIR,
		unix.SYS_CONNECT,
		unix.SYS_BIND,
		unix.SYS_LISTEN,
		unix.SYS_ACCEPT,
		unix.SYS_ACCEPT4,
		unix.SYS_SENDTO,
		unix.SYS_RECVFROM,
		unix.SYS_SENDMSG,
		unix.SYS_RECVMSG,
		unix.SYS_SENDMMSG,
		unix.SYS_RECVMMSG,
	}
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt uint8, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}
