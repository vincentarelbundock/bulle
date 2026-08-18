//go:build linux

package backends

import (
	"fmt"
	"runtime"
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
	bpfJGE = 0x30
	bpfK   = 0x00
	bpfRET = 0x06

	seccompModeFilter = 2
	seccompRetAllow   = 0x7fff0000
	seccompRetErrno   = 0x00050000
	// seccompRetKillProcess terminates the whole process rather than the
	// offending thread. It is what a syscall from an ABI this filter cannot
	// reason about gets: allowing it would be a silent bypass.
	seccompRetKillProcess = 0x80000000

	// Offsets into struct seccomp_data.
	seccompDataNR   = 0
	seccompDataArch = 4

	// x32 syscalls arrive under AUDIT_ARCH_X86_64 with this bit set, and
	// number the same calls differently. Nothing below it is an x32 call.
	x32SyscallBit = 0x40000000
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

// installDenySocketSeccompFilter installs the filter on the calling thread.
//
// PR_SET_SECCOMP is per-thread — the TSYNC flag exists only on seccomp(2) —
// so the caller must have pinned this goroutine to its OS thread and must exec
// from the same thread. Nothing here can enforce that; see landlockBackend.Run.
func installDenySocketSeccompFilter() error {
	if nativeAuditArch == 0 {
		return fmt.Errorf("no seccomp audit architecture is known for %s; refusing to install a filter that cannot check the calling ABI", runtime.GOARCH)
	}
	// Validating seccomp_data.arch first is not optional bookkeeping: syscall
	// numbers are per-ABI, so without it a 32-bit process on an amd64 kernel
	// both escapes the filter (i386 socket is 359, not 41) and is hit by it
	// (i386 41 is dup). Anything that is not the native ABI is killed.
	filters := []unix.SockFilter{
		bpfStmt(bpfLD|bpfW|bpfABS, seccompDataArch),
		bpfJump(bpfJMP|bpfJEQ|bpfK, nativeAuditArch, 0, 0), // jf patched to the kill instruction
		bpfStmt(bpfLD|bpfW|bpfABS, seccompDataNR),
		bpfJump(bpfJMP|bpfJGE|bpfK, x32SyscallBit, 0, 0), // jt patched to the kill instruction
	}
	archJump, x32Jump := 1, 3
	for _, nr := range deniedNetworkSyscalls() {
		filters = append(filters,
			bpfJump(bpfJMP|bpfJEQ|bpfK, uint32(nr), 0, 1),
			bpfStmt(bpfRET|bpfK, seccompRetErrno|uint32(syscall.EPERM)),
		)
	}
	filters = append(filters,
		bpfStmt(bpfRET|bpfK, seccompRetAllow),
		bpfStmt(bpfRET|bpfK, seccompRetKillProcess),
	)
	kill := len(filters) - 1
	// BPF jumps are forward-only offsets from the instruction after the jump,
	// and the offset is a single byte. The filter is far shorter than that, but
	// a silently truncated offset would jump into the middle of the program.
	if kill > 255 {
		return fmt.Errorf("seccomp filter is too long to jump over (%d instructions)", len(filters))
	}
	filters[archJump].Jf = uint8(kill - archJump - 1)
	filters[x32Jump].Jt = uint8(kill - x32Jump - 1)

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
	// SYS_SOCKETPAIR and the send/recv family are deliberately absent. A
	// socketpair is a connected in-process pipe (Linux only supports AF_UNIX
	// pairs) and cannot reach anything outside the sandbox, while async
	// runtimes (tokio, libuv) and signal-handling crates (signal-hook) need
	// one before doing any real work. With socket and connect denied, the
	// only sockets a sandboxed process can hold are connected socketpair
	// ends, and addressed sends on a connected AF_UNIX socket fail with
	// EISCONN, so send*/recv* cannot reach the abstract namespace either.
	return []uintptr{
		unix.SYS_SOCKET,
		unix.SYS_CONNECT,
		unix.SYS_BIND,
		unix.SYS_LISTEN,
		unix.SYS_ACCEPT,
		unix.SYS_ACCEPT4,
		// io_uring queue entries can perform the socket, connect, send, and
		// receive operations without issuing those syscall numbers. In an
		// offline sandbox there are no legitimate inherited rings, so deny the
		// complete ring API rather than leave a second network syscall surface.
		unix.SYS_IO_URING_SETUP,
		unix.SYS_IO_URING_ENTER,
		unix.SYS_IO_URING_REGISTER,
	}
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt uint8, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}
