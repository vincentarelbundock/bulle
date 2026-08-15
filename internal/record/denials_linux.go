//go:build linux

package record

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

const denialLogTimeout = 2 * time.Second

// denialLogSupported reports whether the kernel can log Landlock denials at
// all (ABI v7, Linux 6.15+). Without it there is nothing to read back.
func denialLogSupported() bool {
	abi, err := llsyscall.LandlockGetABIVersion()
	return err == nil && abi >= 7
}

// currentUptime returns seconds since boot, for filtering dmesg timestamps.
func currentUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return uptime
}

// readKernelLog fetches kernel log lines emitted since the given times,
// preferring journalctl (wall-clock filter) and falling back to dmesg
// (boot-relative filter applied later by the parser). Both can fail for
// unprivileged users depending on distro settings; errors are not reported
// because this is a best-effort convenience path.
func readKernelLog(since time.Time) (lines []string, viaDmesg bool) {
	ctx, cancel := context.WithTimeout(context.Background(), denialLogTimeout)
	defer cancel()

	// Denial records reach the journal as _TRANSPORT=audit when journald's
	// audit socket collects them, or as _TRANSPORT=kernel (printk fallback)
	// when nothing listens on the audit netlink socket.
	out, err := exec.CommandContext(ctx, "journalctl", "--quiet", "--no-pager",
		"--output=cat", fmt.Sprintf("--since=@%d", since.Unix()),
		"_TRANSPORT=audit", "+", "_TRANSPORT=kernel").Output()
	if err == nil {
		return strings.Split(string(out), "\n"), false
	}

	out, err = exec.CommandContext(ctx, "dmesg").Output()
	if err == nil {
		return strings.Split(string(out), "\n"), true
	}
	return nil, false
}

type Probe struct {
	start       time.Time
	startUptime float64
	enabled     bool
}

// StartProbe records where the kernel log ends before the sandboxed
// command runs, so only this run's denials are reported afterwards.
func StartProbe(p policy.Policy) Probe {
	if p.Backend != policy.BackendLinuxLandlock || !denialLogSupported() {
		return Probe{}
	}
	return Probe{start: time.Now(), startUptime: currentUptime(), enabled: true}
}

// records returns the denial records the kernel logged since the probe
// started, or nil when logs are unavailable or show no denials.
func (probe Probe) records() []landlockDenial {
	if !probe.enabled {
		return nil
	}
	lines, viaDmesg := readKernelLog(probe.start)
	if lines == nil {
		return nil
	}
	sinceUptime := 0.0
	if viaDmesg {
		sinceUptime = probe.startUptime
	}
	return parseLandlockDenials(lines, sinceUptime)
}

// hints returns copy-pasteable suggestions for sandbox denials logged since
// the probe started, or nil when logs are unavailable or show no denials.
func (probe Probe) Hints() []string {
	home, _ := os.UserHomeDir()
	return denialHints(probe.records(), home)
}

// grants returns the same denials as the policy entries that would allow
// them, for profile recording.
func (probe Probe) Grants() []ObservedGrant {
	return grantsForDenials(probe.records())
}

// Supported reports whether this machine can record a profile, and
// why not when it cannot. Recording reads back what the sandbox refused, so
// without denial logging it would converge instantly on an empty profile that
// looks like success — worth refusing up front rather than discovering later.
func Supported() (string, bool) {
	if policy.RuntimeDefaultBackend() != policy.BackendLinuxLandlock {
		return "recording needs the Landlock backend", false
	}
	if !denialLogSupported() {
		return "recording needs Landlock audit logging (ABI v7, Linux 6.15 or newer)", false
	}
	if lines, _ := readKernelLog(time.Now().Add(-time.Minute)); lines == nil {
		return "recording needs readable kernel logs; neither journalctl nor dmesg would report them here", false
	}
	if !denialLoggingWorks() {
		return "this kernel reports Landlock audit support, but a denial deliberately triggered here never reached the log;" +
			" recording would observe nothing and produce an empty profile that looks like success." +
			" Auditing is most often disabled at boot (audit=0) or filtered by an audit rule", false
	}
	return "", true
}
