//go:build darwin

package app

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

type denialProbe struct {
	start   time.Time
	enabled bool
}

// startDenialProbe records when the sandboxed command starts so only this
// run's Seatbelt violations are reported afterwards. macOS logs sandbox
// denials to the unified log unconditionally, so no capability probing is
// needed.
func startDenialProbe(p policy.Policy) denialProbe {
	if p.Backend != policy.BackendMacOSSeatbelt {
		return denialProbe{}
	}
	return denialProbe{start: time.Now(), enabled: true}
}

// hints returns copy-pasteable suggestions for sandbox denials logged since
// the probe started, or nil when the log is unreadable or shows no denials.
// Best-effort: violation records are written asynchronously, so denials from
// the very end of a run can be missed.
func (probe denialProbe) hints() []string {
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
	home, _ := os.UserHomeDir()
	return seatbeltHints(parseSeatbeltDenials(strings.Split(string(out), "\n")), home)
}
