package limits

import (
	"fmt"
	"time"
)

// Mechanism names the kernel facility behind an enforced limit. It is part of
// the user-visible policy output because "capped at 4G" means something
// materially different depending on which one applies.
type Mechanism string

const (
	// MechanismCgroup is cgroup v2, which counts the sandbox's process tree
	// and nothing else.
	MechanismCgroup Mechanism = "cgroup v2"
	// MechanismRlimit is a POSIX resource limit set on the sandboxed process
	// before it execs.
	MechanismRlimit Mechanism = "rlimit"
	// MechanismNone marks a requested limit that is not enforced here.
	MechanismNone Mechanism = ""
)

// Status is the resolved fate of one requested limit on this machine.
type Status struct {
	// Name is the limit's flag name without the leading dashes, which is also
	// its configuration key.
	Name string `json:"name"`
	// Value is the limit as the user wrote it, normalized.
	Value string `json:"value"`
	// Mechanism is what enforces it, or MechanismNone when nothing does.
	Mechanism Mechanism `json:"mechanism,omitempty"`
	// Enforced reports whether the limit actually binds on this run.
	Enforced bool `json:"enforced"`
	// Reason explains an unenforced limit in one sentence, suitable for
	// printing after "bulle: ".
	Reason string `json:"reason,omitempty"`
}

// Note renders an unenforced status as a stderr warning line.
func (s Status) Note() string {
	return fmt.Sprintf("--%s is not enforced here: %s", s.Name, s.Reason)
}

// Support describes what this machine can enforce. It is resolved once per run
// so the warning, the policy output, and the enforcement path cannot disagree.
type Support struct {
	// GOOS is the platform being reported on.
	GOOS string
	// Cgroup reports whether a writable cgroup v2 subtree is available for
	// bulle to create the run's cgroup in.
	Cgroup bool
	// CgroupReason explains an unavailable cgroup, when the platform could
	// otherwise have had one.
	CgroupReason string
}

// Plan resolves each requested limit against what this machine supports,
// in a stable order. Limits that were not requested are omitted entirely,
// which is what keeps a platform-scoped configuration quiet: a limit set only
// under [defaults.linux] never reaches Plan on macOS, so it never warns.
func Plan(l Limits, support Support) []Status {
	var statuses []Status

	add := func(name string, value string, cgroupBacked bool) {
		if value == "" {
			return
		}
		status := Status{Name: name, Value: value}
		switch {
		case !cgroupBacked:
			status.Mechanism = MechanismRlimit
			status.Enforced = true
		case support.Cgroup:
			status.Mechanism = MechanismCgroup
			status.Enforced = true
		default:
			status.Mechanism = MechanismNone
			status.Reason = unsupportedReason(name, support)
		}
		statuses = append(statuses, status)
	}

	add("memory", formatSize(l.Memory), true)
	add("cpu", formatPercent(l.CPU), true)
	add("nproc", formatCount(l.NProc), true)
	add("nofile", formatCount(l.NoFile), false)
	add("fsize", formatSize(l.FSize), false)
	add("cpu-time", formatDuration(l.CPUTime), false)

	return statuses
}

// unsupportedReason explains why a cgroup-backed limit does not bind. The
// macOS wording says what is missing rather than only that it is missing,
// because the natural next question is whether some other flag would work —
// and for these three, none would.
func unsupportedReason(name string, support Support) string {
	if support.GOOS == "darwin" {
		switch name {
		case "memory":
			return "macOS has no per-process-tree memory cap (Seatbelt has no resource controls, and RLIMIT_AS would cap virtual address space, killing runtimes that merely reserve it)"
		case "cpu":
			return "macOS has no per-process-tree CPU quota (Seatbelt has no resource controls)"
		case "nproc":
			return "macOS has no per-process-tree process cap (RLIMIT_NPROC counts every process owned by your user, so it would throttle your other programs too)"
		}
		return "macOS has no equivalent mechanism"
	}
	if support.CgroupReason != "" {
		return "no writable cgroup v2 delegation (" + support.CgroupReason + ")"
	}
	return "no writable cgroup v2 delegation"
}

// Unenforced returns the statuses that do not bind, for warning and for
// --strict-limits.
func Unenforced(statuses []Status) []Status {
	var out []Status
	for _, status := range statuses {
		if !status.Enforced {
			out = append(out, status)
		}
	}
	return out
}

func formatSize(bytes uint64) string {
	if bytes == 0 {
		return ""
	}
	units := []struct {
		suffix string
		scale  uint64
	}{
		{"T", 1 << 40},
		{"G", 1 << 30},
		{"M", 1 << 20},
		{"K", 1 << 10},
	}
	for _, unit := range units {
		if bytes >= unit.scale && bytes%unit.scale == 0 {
			return fmt.Sprintf("%d%s", bytes/unit.scale, unit.suffix)
		}
	}
	return fmt.Sprintf("%dB", bytes)
}

func formatPercent(percent uint64) string {
	if percent == 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", percent)
}

func formatCount(count uint64) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d", count)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}
