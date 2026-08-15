//go:build darwin

package record

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

// log show has to scan the unified log store, which routinely takes a few
// seconds even for a narrow window; this only runs after a failed sandboxed
// command, so the wait buys actionable hints.
const denialLogTimeout = 8 * time.Second

type Probe struct {
	start   time.Time
	enabled bool
}

// StartProbe records when the sandboxed command starts so only this
// run's Seatbelt violations are reported afterwards. macOS logs sandbox
// denials to the unified log unconditionally, so no capability probing is
// needed.
func StartProbe(p policy.Policy) Probe {
	if p.Backend != policy.BackendMacOSSeatbelt {
		return Probe{}
	}
	return Probe{start: time.Now(), enabled: true}
}

// Hints returns copy-pasteable suggestions for sandbox denials logged since
// the probe started, or nil when the log is unreadable or shows no denials.
// Best-effort: violation records are written asynchronously, so denials from
// the very end of a run can be missed.
func (probe Probe) Hints() []string {
	home, _ := os.UserHomeDir()
	return seatbeltHints(probe.records(), home)
}

// records returns the Seatbelt violations logged since the probe started.
func (probe Probe) records() []seatbeltDenial {
	if !probe.enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), denialLogTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "log", "show",
		"--style", "syslog",
		"--start", probe.start.Format("2006-01-02 15:04:05"),
		"--predicate", `sender == "Sandbox" OR process == "sandboxd"`).Output()
	if err != nil {
		return nil
	}
	return parseSeatbeltDenials(strings.Split(string(out), "\n"))
}

// Grants returns the same violations as the policy entries that would allow
// them, for profile recording.
//
// Attribution is the honest limitation on macOS. The unified log reports
// violations from every sandboxed process on the machine, and a violation
// names a pid that has already exited by the time the log is read, so it
// cannot be traced back to this run's process group. Rather than guess — a
// filter by process name would drop real grants from a command's helpers —
// recording keeps the process name on each entry and says so in the output.
func (probe Probe) Grants() []ObservedGrant {
	return grantsForSeatbeltDenials(probe.records())
}
