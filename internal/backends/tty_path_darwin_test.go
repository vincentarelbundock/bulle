//go:build darwin

package backends

import (
	"os"
	"strconv"
	"testing"
)

// fcntlGetPath must resolve character devices, which is exactly what the broken
// os.Readlink("/dev/fd/N") approach could not do on macOS. /dev/null is the
// simplest character device with a stable path.
func TestFcntlGetPathResolvesCharacterDevice(t *testing.T) {
	f, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Document the old failure mode: /dev/fd/N is not a symlink on macOS.
	if _, err := os.Readlink("/dev/fd/" + strconv.Itoa(int(f.Fd()))); err == nil {
		t.Fatal("expected os.Readlink on /dev/fd/N to fail; the regression assumed it returned a symlink target")
	}

	got, ok := fcntlGetPath(int(f.Fd()))
	if !ok {
		t.Fatal("fcntlGetPath returned ok=false for /dev/null")
	}
	if got != "/dev/null" {
		t.Fatalf("fcntlGetPath = %q, want /dev/null", got)
	}
}
