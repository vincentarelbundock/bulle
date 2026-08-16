//go:build linux

package backends

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// nativeAuditArch is the AUDIT_ARCH value a syscall issued by this build's ABI
// carries in seccomp_data.arch. A filter that does not check it is comparing
// syscall numbers from one ABI against the numbers of another, which is both a
// bypass (the number it wants to deny is a different number there) and a
// misfire (the number it denies means something harmless there).
var nativeAuditArch = auditArchFor(runtime.GOARCH)

func auditArchFor(goarch string) uint32 {
	switch goarch {
	case "amd64":
		return uint32(unix.AUDIT_ARCH_X86_64)
	case "arm64":
		return uint32(unix.AUDIT_ARCH_AARCH64)
	case "386":
		return uint32(unix.AUDIT_ARCH_I386)
	case "arm":
		return uint32(unix.AUDIT_ARCH_ARM)
	case "riscv64":
		return uint32(unix.AUDIT_ARCH_RISCV64)
	case "ppc64le":
		return uint32(unix.AUDIT_ARCH_PPC64LE)
	case "s390x":
		return uint32(unix.AUDIT_ARCH_S390X)
	}
	return 0
}
