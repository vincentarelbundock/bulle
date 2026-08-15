//go:build linux

package backends

import "github.com/vincentarelbundock/bulle/internal/policy"

// ApplyFilesystemRestrictions applies a policy's filesystem rules to the
// calling process, with the same configuration and logging behavior a
// sandboxed run uses. It exists so the recording precondition check can be a
// real sandboxed run rather than an imitation of one: a probe that restricted
// itself differently from a run would answer a different question than the one
// asked.
//
// The restriction is irreversible for the calling process, so callers are
// expected to be a short-lived child that exits or execs immediately after.
func ApplyFilesystemRestrictions(p policy.Policy) error {
	return applyLandlockFilesystem(p)
}
