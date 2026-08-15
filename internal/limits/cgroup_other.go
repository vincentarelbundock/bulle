//go:build !linux

package limits

import "errors"

// Cgroup exists on every platform so callers need no build tags, but off Linux
// it is never constructed.
type Cgroup struct{}

// Detect reports no cgroup support. On macOS this is a permanent property of
// the platform rather than a missing delegation, so no reason is recorded: the
// per-limit explanations in unsupportedReason are more specific than anything
// that could be said here.
func Detect(goos string) Support {
	return Support{GOOS: goos}
}

func Create(l Limits) (*Cgroup, error) {
	return nil, errors.New("cgroups are only available on Linux")
}

func (c *Cgroup) FD() int { return 0 }

func (c *Cgroup) Kill() error { return nil }

func (c *Cgroup) Close() error { return nil }
