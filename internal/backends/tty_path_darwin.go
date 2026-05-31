//go:build darwin

package backends

import (
	"syscall"
	"unsafe"
)

// fcntlGetPath returns the filesystem path backing an open file descriptor via
// macOS fcntl(F_GETPATH). Unlike reading /dev/fd/N, this resolves character
// devices, so it maps the inherited terminal descriptor back to its
// /dev/ttysNNN device node. On macOS /dev/fd/N entries are device nodes rather
// than symlinks, so os.Readlink cannot do this. ok is false when the descriptor
// has no path (for example a pipe) or the call fails.
func fcntlGetPath(fd int) (path string, ok bool) {
	var buf [1024]byte // MAXPATHLEN on darwin
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETPATH), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return "", false
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n]), true
}
