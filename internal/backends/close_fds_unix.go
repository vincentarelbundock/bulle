//go:build linux || darwin

package backends

import "syscall"

// fallbackFDCeiling returns the upper bound (exclusive) for the fixed-range fd
// sweep used when the platform's fd directory is unreadable. It uses the soft
// RLIMIT_NOFILE so every potentially-open descriptor is covered, with a sane
// floor and an upper cap to bound the loop when the limit is unlimited.
// Hardcoding 1024 instead would silently leak inherited descriptors into the
// sandboxed command wherever the limit has been raised, which is routine under
// launchd and systemd alike.
func fallbackFDCeiling() int {
	const floor = 1024
	const ceiling = 1 << 20
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return floor
	}
	// lim.Cur is unsigned; compare before any narrowing conversion so that
	// RLIM_INFINITY (math.MaxUint64) does not wrap to a small or negative int.
	if lim.Cur > uint64(ceiling) {
		return ceiling
	}
	if lim.Cur < uint64(floor) {
		return floor
	}
	return int(lim.Cur)
}
