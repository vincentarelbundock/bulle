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

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
	"github.com/vincentarelbundock/bulle/internal/trustedexec"
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
	journalctl, err := trustedexec.First(
		"/usr/bin/journalctl",
		"/bin/journalctl",
		"/run/current-system/sw/bin/journalctl",
	)
	if err == nil {
		out, runErr := exec.CommandContext(ctx, journalctl, "--quiet", "--no-pager",
			"--output=cat", fmt.Sprintf("--since=@%d", since.Unix()),
			"_TRANSPORT=audit", "+", "_TRANSPORT=kernel").Output()
		if runErr == nil {
			return strings.Split(string(out), "\n"), false
		}
	}

	dmesg, err := trustedexec.First(
		"/usr/bin/dmesg",
		"/bin/dmesg",
		"/run/current-system/sw/bin/dmesg",
	)
	if err == nil {
		out, runErr := exec.CommandContext(ctx, dmesg).Output()
		if runErr == nil {
			return strings.Split(string(out), "\n"), true
		}
	}
	return nil, false
}

type Probe struct {
	start       time.Time
	startUptime float64
	enabled     bool
	marker      string
}

// StartProbe records where the kernel log ends before the sandboxed
// command runs, so only this run's denials are reported afterwards.
func StartProbe(p *policy.Policy) Probe {
	if p == nil || p.Backend != policy.BackendLinuxLandlock || !denialLogSupported() {
		return Probe{}
	}
	file, err := os.CreateTemp("", ".bulle-landlock-audit-*")
	if err != nil {
		return Probe{}
	}
	marker := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(marker)
		return Probe{}
	}
	granted := append([]string{}, p.ReadOnly...)
	granted = append(granted, p.ReadOnlyExec...)
	granted = append(granted, p.ReadWrite...)
	granted = append(granted, p.ReadWriteExec...)
	if bpaths.IsWithinAnyRootResolvingSymlinks(marker, granted) {
		_ = os.Remove(marker)
		return Probe{}
	}
	p.AuditMarker = marker
	return Probe{start: time.Now(), startUptime: currentUptime(), enabled: true, marker: marker}
}

func (probe Probe) Close() {
	if probe.marker != "" {
		_ = os.Remove(probe.marker)
	}
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
	return denialsForMarkerDomain(parseLandlockDenials(lines, sinceUptime), probe.marker)
}

func denialsForMarkerDomain(all []landlockDenial, marker string) []landlockDenial {
	domain := ""
	for _, denial := range all {
		if denial.Path == marker {
			domain = denial.Domain
			break
		}
	}
	if domain == "" {
		return nil
	}
	out := make([]landlockDenial, 0, len(all))
	for _, denial := range all {
		if denial.Domain == domain && denial.Path != marker {
			out = append(out, denial)
		}
	}
	return out
}

// Hints returns copy-pasteable suggestions for sandbox denials logged since
// the probe started, or nil when logs are unavailable or show no denials.
func (probe Probe) Hints() []string {
	home, _ := os.UserHomeDir()
	return denialHints(probe.records(), home)
}

// Grants returns the same denials as the policy entries that would allow
// them, for profile recording.
func (probe Probe) Grants() []ObservedGrant {
	return grantsForDenials(probe.records())
}
