package limits

import (
	"strings"
	"testing"
	"time"
)

func TestParseAcceptsSizesPercentsAndDurations(t *testing.T) {
	got, err := Parse(Spec{
		Memory:  "4G",
		CPU:     "200%",
		NProc:   "512",
		NoFile:  "4096",
		FSize:   "100M",
		CPUTime: "90s",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Limits{
		Memory:  4 << 30,
		CPU:     200,
		NProc:   512,
		NoFile:  4096,
		FSize:   100 << 20,
		CPUTime: 90 * time.Second,
	}
	if got != want {
		t.Fatalf("Parse = %+v, want %+v", got, want)
	}
}

func TestParseSizeUnits(t *testing.T) {
	cases := map[string]uint64{
		"1024":  1024,
		"1K":    1 << 10,
		"1KiB":  1 << 10,
		"1kb":   1 << 10,
		"512M":  512 << 20,
		"1.5G":  1536 << 20,
		"2GiB":  2 << 30,
		"1T":    1 << 40,
		"4096B": 4096,
	}
	for value, want := range cases {
		got, err := Parse(Spec{Memory: value})
		if err != nil {
			t.Errorf("Parse(%q): %v", value, err)
			continue
		}
		if got.Memory != want {
			t.Errorf("Parse(%q).Memory = %d, want %d", value, got.Memory, want)
		}
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		spec Spec
		want string
	}{
		{Spec{Memory: "4Q"}, "--memory"},
		{Spec{Memory: "-1G"}, "--memory"},
		// A bare number is ambiguous between a core count and a percentage, so
		// it is refused rather than guessed.
		{Spec{CPU: "2"}, "--cpu"},
		{Spec{CPU: "0%"}, "--cpu"},
		{Spec{NProc: "many"}, "--nproc"},
		{Spec{NoFile: "-4"}, "--nofile"},
		{Spec{CPUTime: "90"}, "--cpu-time"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.spec)
		if err == nil {
			t.Errorf("Parse(%+v) succeeded, want an error", tc.spec)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%+v) error = %q, want it to name %s", tc.spec, err, tc.want)
		}
	}
}

// A zero value disables a limit, matching --timeout 0.
func TestParseTreatsZeroAndEmptyAsUnset(t *testing.T) {
	for _, spec := range []Spec{{}, {Memory: "0", CPU: "0", NProc: "0", NoFile: "0", FSize: "0", CPUTime: "0"}} {
		got, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%+v): %v", spec, err)
		}
		if !got.Empty() {
			t.Errorf("Parse(%+v) = %+v, want no limits", spec, got)
		}
	}
}

// RLIMIT_CPU counts whole seconds, so a sub-second remainder must round up
// rather than silently truncating toward a stricter limit than requested.
func TestParseRoundsCPUTimeUpToWholeSeconds(t *testing.T) {
	got, err := Parse(Spec{CPUTime: "1500ms"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.CPUTime != 2*time.Second {
		t.Fatalf("CPUTime = %v, want 2s", got.CPUTime)
	}
}

func TestMergeLetsTheOverridingSpecWinPerField(t *testing.T) {
	base := Spec{Memory: "1G", NProc: "100"}
	got := base.Merge(Spec{Memory: "4G", NoFile: "2048"})
	want := Spec{Memory: "4G", NProc: "100", NoFile: "2048"}
	if got != want {
		t.Fatalf("Merge = %+v, want %+v", got, want)
	}
}

func TestPlanMarksCgroupLimitsUnenforcedOnMacOS(t *testing.T) {
	l := Limits{Memory: 4 << 30, CPU: 200, NProc: 512, NoFile: 4096}
	statuses := Plan(l, Support{GOOS: "darwin"})
	if len(statuses) != 4 {
		t.Fatalf("Plan returned %d statuses, want 4", len(statuses))
	}
	byName := map[string]Status{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	for _, name := range []string{"memory", "cpu", "nproc"} {
		status := byName[name]
		if status.Enforced {
			t.Errorf("%s reported as enforced on macOS", name)
		}
		if status.Mechanism != MechanismNone {
			t.Errorf("%s mechanism = %q, want none", name, status.Mechanism)
		}
		if !strings.Contains(status.Reason, "macOS") {
			t.Errorf("%s reason = %q, want it to name the platform", name, status.Reason)
		}
	}
	// The portable limits still bind on macOS; only the cgroup-backed ones warn.
	if nofile := byName["nofile"]; !nofile.Enforced || nofile.Mechanism != MechanismRlimit {
		t.Errorf("nofile = %+v, want an enforced rlimit", nofile)
	}
}

// The macOS explanation for nproc must say why the obvious fallback is wrong,
// since RLIMIT_NPROC exists there and looks like it would do the job.
func TestPlanExplainsWhyNProcHasNoMacOSFallback(t *testing.T) {
	statuses := Plan(Limits{NProc: 512}, Support{GOOS: "darwin"})
	if len(statuses) != 1 {
		t.Fatalf("Plan returned %d statuses, want 1", len(statuses))
	}
	if !strings.Contains(statuses[0].Reason, "RLIMIT_NPROC") {
		t.Errorf("reason = %q, want it to explain the RLIMIT_NPROC fallback", statuses[0].Reason)
	}
}

func TestPlanReportsCgroupLimitsEnforcedWhenDelegated(t *testing.T) {
	statuses := Plan(Limits{Memory: 4 << 30}, Support{GOOS: "linux", Cgroup: true})
	if len(statuses) != 1 {
		t.Fatalf("Plan returned %d statuses, want 1", len(statuses))
	}
	if !statuses[0].Enforced || statuses[0].Mechanism != MechanismCgroup {
		t.Fatalf("memory = %+v, want an enforced cgroup limit", statuses[0])
	}
}

func TestPlanReportsMissingDelegationOnLinux(t *testing.T) {
	support := Support{GOOS: "linux", CgroupReason: "no delegated cgroup is writable by this user"}
	statuses := Plan(Limits{Memory: 4 << 30}, support)
	if statuses[0].Enforced {
		t.Fatal("memory reported as enforced without a delegated cgroup")
	}
	if !strings.Contains(statuses[0].Reason, "cgroup v2") {
		t.Errorf("reason = %q, want it to name the missing mechanism", statuses[0].Reason)
	}
	if !strings.Contains(statuses[0].Reason, support.CgroupReason) {
		t.Errorf("reason = %q, want it to include the specific cause", statuses[0].Reason)
	}
}

// A limit that was never requested must not appear at all: this is what keeps
// a platform-scoped configuration silent on the platform it does not target.
func TestPlanOmitsUnrequestedLimits(t *testing.T) {
	if statuses := Plan(Limits{}, Support{GOOS: "darwin"}); len(statuses) != 0 {
		t.Fatalf("Plan returned %d statuses for no limits, want 0", len(statuses))
	}
}

func TestUnenforcedSelectsOnlyTheLimitsThatDoNotBind(t *testing.T) {
	statuses := Plan(Limits{Memory: 1 << 30, NoFile: 4096}, Support{GOOS: "darwin"})
	unenforced := Unenforced(statuses)
	if len(unenforced) != 1 || unenforced[0].Name != "memory" {
		t.Fatalf("Unenforced = %+v, want only memory", unenforced)
	}
	if note := unenforced[0].Note(); !strings.HasPrefix(note, "--memory is not enforced here") {
		t.Errorf("Note = %q, want it to name the flag", note)
	}
}

func TestStatusValueRoundTripsThroughFormatting(t *testing.T) {
	l, err := Parse(Spec{Memory: "4G", CPU: "200%", NProc: "512", CPUTime: "1h30m"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	statuses := Plan(l, Support{GOOS: "linux", Cgroup: true})
	want := map[string]string{"memory": "4G", "cpu": "200%", "nproc": "512", "cpu-time": "1h30m0s"}
	for _, status := range statuses {
		if want[status.Name] != status.Value {
			t.Errorf("%s value = %q, want %q", status.Name, status.Value, want[status.Name])
		}
	}
}
