//go:build darwin

package integration

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// macOS pty control ioctls from <sys/ttycom.h>.
const (
	tiocPtyGrant = 0x20007454 // _IO('t', 84)
	tiocPtyUnlk  = 0x20007452 // _IO('t', 82)
	tiocPtyGname = 0x40807453 // _IOR('t', 83, char[128])
)

// openPTY allocates a pseudo-terminal and returns the master and slave files.
// It works even when the test process has no controlling terminal.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open /dev/ptmx: %v", err)
	}
	for _, req := range []uintptr{tiocPtyGrant, tiocPtyUnlk} {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), req, 0); e != 0 {
			m.Close()
			t.Fatalf("ioctl 0x%x on ptmx: %v", req, e)
		}
	}
	var nameBuf [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), tiocPtyGname, uintptr(unsafe.Pointer(&nameBuf[0]))); e != 0 {
		m.Close()
		t.Fatalf("ioctl TIOCPTYGNAME: %v", e)
	}
	name := string(nameBuf[:bytes.IndexByte(nameBuf[:], 0)])
	s, err := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Fatalf("open pty slave %q: %v", name, err)
	}
	return m, s
}

// TestMacOSSeatbeltAllowsInheritedTerminalIoctl reproduces the broken-TUI bug:
// an interactive tool inherits the terminal as stdin and must perform a termios
// ioctl (raw mode) on it. The seatbelt profile must grant file-ioctl on the
// inherited /dev/ttysNNN device, or the ioctl is denied and the TUI breaks.
func TestMacOSSeatbeltAllowsInheritedTerminalIoctl(t *testing.T) {
	requireSandboxExec(t)
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	project := t.TempDir()

	master, slave := openPTY(t)
	defer master.Close()

	// stty -g reads termios via ioctl(TIOCGETA) on stdin; it succeeds only when
	// the sandbox grants file-ioctl on the inherited terminal device.
	cmd := exec.Command(bin, project, "--rox", "/bin", "--",
		"/bin/sh", "-c", "/bin/stty -g >/dev/null 2>&1 && printf RAWOK || printf RAWFAIL")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	if err := cmd.Start(); err != nil {
		slave.Close()
		t.Fatalf("start: %v", err)
	}
	slave.Close() // drop our copy so the master sees EOF once the child exits

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, master)
		close(done)
	}()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("sandboxed command failed: %v, output: %q", err, buf.String())
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		master.Close()
		<-done
	}

	if out := strings.TrimRight(buf.String(), "\r\n"); !strings.Contains(out, "RAWOK") {
		t.Fatalf("terminal ioctl was denied inside the sandbox: got %q, want RAWOK", out)
	}
}
