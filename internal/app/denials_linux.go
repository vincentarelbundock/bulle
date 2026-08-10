//go:build linux

package app

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

type denialProbe struct {
	start       time.Time
	startUptime float64
	enabled     bool
}

// startDenialProbe records where the kernel log ends before the sandboxed
// command runs, so only this run's denials are reported afterwards.
func startDenialProbe(p policy.Policy) denialProbe {
	if p.Backend != policy.BackendLinuxLandlock || !denialLogSupported() {
		return denialProbe{}
	}
	return denialProbe{start: time.Now(), startUptime: currentUptime(), enabled: true}
}

// hints returns copy-pasteable suggestions for sandbox denials logged since
// the probe started, or nil when logs are unavailable or show no denials.
func (probe denialProbe) hints() []string {
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
	home, _ := os.UserHomeDir()
	return denialHints(parseLandlockDenials(lines, sinceUptime), home)
}
