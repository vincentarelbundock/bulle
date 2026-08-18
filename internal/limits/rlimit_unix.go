//go:build linux || darwin

package limits

import (
	"fmt"
	"math"
	"syscall"
)

// rlimitInfinity is the hard-limit value meaning "no ceiling". RLIM_INFINITY
// is defined as -1 and stored in an unsigned field, so it reads back as the
// maximum value.
const rlimitInfinity = uint64(math.MaxUint64)

// ApplyRlimits sets the portable limits on the calling process. It runs in the
// sandboxed child just before the sandbox itself is applied, so the limits are
// inherited across the final exec but never constrain bulle's own supervisor.
func ApplyRlimits(l Limits) error {
	if l.NoFile > 0 {
		if err := setLimit(syscall.RLIMIT_NOFILE, l.NoFile, "nofile"); err != nil {
			return err
		}
	}
	if l.FSize > 0 {
		if err := setLimit(syscall.RLIMIT_FSIZE, l.FSize, "fsize"); err != nil {
			return err
		}
	}
	if l.CPUTime > 0 {
		if err := setLimit(syscall.RLIMIT_CPU, uint64(l.CPUTime.Seconds()), "cpu-time"); err != nil {
			return err
		}
	}
	return nil
}

// setLimit irreversibly lowers both limits to value, clamped to the inherited hard
// limit. Asking for more than the hard limit is not something the user can act
// on from inside bulle — the shell that launched it set the ceiling — so the
// request is honored as far as the kernel allows rather than failing the run.
func setLimit(resource int, value uint64, name string) error {
	var current syscall.Rlimit
	if err := syscall.Getrlimit(resource, &current); err != nil {
		return fmt.Errorf("read the current %s limit: %w", name, err)
	}
	limit := value
	if hard := uint64(current.Max); hard != rlimitInfinity && limit > hard {
		limit = hard
	}
	updated := current
	updated.Cur = limit
	updated.Max = limit
	if err := syscall.Setrlimit(resource, &updated); err != nil {
		return fmt.Errorf("set the %s limit to %d: %w", name, limit, err)
	}
	return nil
}
