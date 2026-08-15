package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/limits"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestWriteLimitsNamesTheMechanismBehindEachLimit(t *testing.T) {
	var out bytes.Buffer
	writeLimits(&out, limits.Plan(
		limits.Limits{Memory: 4 << 30, NoFile: 4096},
		limits.Support{GOOS: "linux", Cgroup: true},
	))
	text := out.String()
	if !strings.Contains(text, "memory:") || !strings.Contains(text, "cgroup v2") {
		t.Errorf("summary = %q, want the memory limit and its mechanism", text)
	}
	if !strings.Contains(text, "nofile:") || !strings.Contains(text, "rlimit") {
		t.Errorf("summary = %q, want the nofile limit and its mechanism", text)
	}
}

// An unenforced limit must be conspicuous in the policy output: reading
// "memory: 4G" and assuming a cap that does not exist is the failure this
// whole feature is trying to prevent.
func TestWriteLimitsMarksUnenforcedLimitsLoudly(t *testing.T) {
	var out bytes.Buffer
	writeLimits(&out, limits.Plan(limits.Limits{Memory: 4 << 30}, limits.Support{GOOS: "darwin"}))
	text := out.String()
	if !strings.Contains(text, "NOT ENFORCED") {
		t.Errorf("summary = %q, want it to flag the limit as unenforced", text)
	}
	if !strings.Contains(text, "macOS") {
		t.Errorf("summary = %q, want it to explain why", text)
	}
}

func TestWriteLimitsPrintsNothingWhenNoLimitIsRequested(t *testing.T) {
	var out bytes.Buffer
	writeLimits(&out, limits.Plan(limits.Limits{}, limits.Support{GOOS: "darwin"}))
	if out.Len() != 0 {
		t.Fatalf("summary = %q, want no output", out.String())
	}
}

func TestReportUnenforcedLimitsAllowsARunWithNoLimits(t *testing.T) {
	var stderr bytes.Buffer
	code, ok := reportUnenforcedLimits(policy.Policy{}, true, &stderr)
	if !ok || code != ExitOK {
		t.Fatalf("reportUnenforcedLimits = (%d, %v), want (0, true)", code, ok)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence", stderr.String())
	}
}

// The portable limits bind everywhere, so requesting only those must never
// produce a warning on any platform.
func TestReportUnenforcedLimitsStaysSilentForPortableLimits(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{Limits: limits.Limits{NoFile: 4096, FSize: 1 << 20}}
	code, ok := reportUnenforcedLimits(p, true, &stderr)
	if !ok || code != ExitOK {
		t.Fatalf("reportUnenforcedLimits = (%d, %v), want (0, true)", code, ok)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want silence", stderr.String())
	}
}
