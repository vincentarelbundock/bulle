//go:build linux || darwin

package limits

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestAppliedRlimitCannotBeRaisedByChild(t *testing.T) {
	if os.Getenv("BULLE_RLIMIT_HELPER") == "1" {
		var inherited syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &inherited); err != nil {
			os.Exit(10)
		}
		if inherited.Max <= 64 {
			os.Exit(0)
		}
		if err := ApplyRlimits(Limits{NoFile: 64}); err != nil {
			os.Exit(11)
		}
		var applied syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &applied); err != nil || applied.Cur != 64 || applied.Max != 64 {
			os.Exit(12)
		}
		raised := syscall.Rlimit{Cur: 65, Max: 65}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &raised); !errors.Is(err, syscall.EPERM) {
			os.Exit(13)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAppliedRlimitCannotBeRaisedByChild$")
	cmd.Env = append(os.Environ(), "BULLE_RLIMIT_HELPER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rlimit helper: %v\n%s", err, out)
	}
}
