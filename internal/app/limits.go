package app

import (
	"fmt"
	"io"

	"github.com/vincentarelbundock/bulle/internal/exitcode"
	"github.com/vincentarelbundock/bulle/internal/limits"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// reportUnenforcedLimits warns about every requested resource limit that does
// not bind on this machine, and reports whether the run may proceed.
//
// The default is to warn and continue, so that one configuration can be shared
// between a Linux workstation and a Mac laptop without the weaker platform
// refusing to start. That trade has a real cost — a warning printed on every
// run stops being read — so --strict-limits promotes it to a refusal for the
// cases where an unenforced limit means the run should not happen at all.
func reportUnenforcedLimits(p policy.Policy, strict bool, stderr io.Writer) (int, bool) {
	unenforced := limits.Unenforced(limits.Plan(p.Limits, limits.Current()))
	if len(unenforced) == 0 {
		return exitcode.OK, true
	}
	for _, status := range unenforced {
		fmt.Fprintf(stderr, "bulle: %s\n", status.Note())
	}
	if !strict {
		return exitcode.OK, true
	}
	fmt.Fprintln(stderr, "bulle: refusing to run because --strict-limits is set; drop the limit, scope it to the platform that enforces it, or clear --strict-limits")
	return exitcode.ConfigError, false
}
