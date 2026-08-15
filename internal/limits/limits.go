// Package limits models the resource constraints bulle can place on a
// sandboxed run: how they are written on the command line and in the
// configuration, what the kernel mechanism behind each one is, and — because
// the mechanisms differ per platform — which of them are actually enforced
// here.
//
// The split that shapes this package is between limits that need cgroup v2 and
// limits that a plain setrlimit can express. Memory, CPU share, and process
// count are cgroup-only on purpose. RLIMIT_AS caps virtual address space
// rather than resident memory, which misfires badly on runtimes that reserve
// large sparse mappings (Go, the JVM, Node); RLIMIT_NPROC counts processes per
// real UID across the whole system, so a sandbox limit would throttle the
// user's unrelated programs. Neither is a per-tree limit, and promising one
// while delivering the other would be worse than declining. So on macOS, and
// on Linux without a writable cgroup v2 delegation, those three are reported
// as unenforced instead of silently downgraded.
package limits

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Spec is the unparsed form of the limits, as written by a flag or a
// configuration key. Keeping the raw strings lets flags and [defaults] merge
// before anything is validated, so an invalid value is reported once, against
// the value that actually won.
type Spec struct {
	Memory  string
	CPU     string
	NProc   string
	NoFile  string
	FSize   string
	CPUTime string
}

// Empty reports whether the spec requests no limits at all.
func (s Spec) Empty() bool {
	return s == Spec{}
}

// Merge returns s with every field set in over replaced by over's value. The
// caller merges the lower-precedence spec into the higher one, so an explicit
// flag beats a platform-scoped default, which beats an unscoped default.
func (s Spec) Merge(over Spec) Spec {
	return Spec{
		Memory:  choose(over.Memory, s.Memory),
		CPU:     choose(over.CPU, s.CPU),
		NProc:   choose(over.NProc, s.NProc),
		NoFile:  choose(over.NoFile, s.NoFile),
		FSize:   choose(over.FSize, s.FSize),
		CPUTime: choose(over.CPUTime, s.CPUTime),
	}
}

func choose(preferred string, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

// Limits is the parsed, validated form. A zero field means "not requested";
// there is deliberately no way to express "unlimited" beyond omitting the
// limit, since bulle never raises a limit the parent shell already set.
type Limits struct {
	// Memory is a resident-memory cap in bytes, enforced by cgroup memory.max.
	Memory uint64 `json:"memory,omitempty"`
	// CPU is a CPU quota in percent of one core (200 means two full cores),
	// enforced by cgroup cpu.max.
	CPU uint64 `json:"cpu,omitempty"`
	// NProc is the maximum number of processes in the sandbox, enforced by
	// cgroup pids.max. It is per process tree, not per UID.
	NProc uint64 `json:"nproc,omitempty"`
	// NoFile is the maximum open file descriptors, enforced by RLIMIT_NOFILE.
	NoFile uint64 `json:"nofile,omitempty"`
	// FSize is the maximum size of any single file the sandbox writes, in
	// bytes, enforced by RLIMIT_FSIZE.
	FSize uint64 `json:"fsize,omitempty"`
	// CPUTime is consumed CPU time (not wall clock), enforced by RLIMIT_CPU,
	// which has one-second granularity.
	CPUTime time.Duration `json:"cpu_time,omitempty"`
}

// Empty reports whether no limit is requested.
func (l Limits) Empty() bool {
	return l == Limits{}
}

// Parse validates a spec. Every error names the flag, the offending value, and
// an accepted form, matching how --timeout reports a bad duration.
func Parse(s Spec) (Limits, error) {
	var l Limits
	var err error

	if l.Memory, err = parseSize(s.Memory, "--memory"); err != nil {
		return Limits{}, err
	}
	if l.CPU, err = parsePercent(s.CPU, "--cpu"); err != nil {
		return Limits{}, err
	}
	if l.NProc, err = parseCount(s.NProc, "--nproc"); err != nil {
		return Limits{}, err
	}
	if l.NoFile, err = parseCount(s.NoFile, "--nofile"); err != nil {
		return Limits{}, err
	}
	if l.FSize, err = parseSize(s.FSize, "--fsize"); err != nil {
		return Limits{}, err
	}
	if l.CPUTime, err = parseCPUTime(s.CPUTime, "--cpu-time"); err != nil {
		return Limits{}, err
	}
	return l, nil
}

// unset reports whether a raw value means "no limit". "0" is accepted as a
// disabler for symmetry with --timeout 0.
func unset(value string) bool {
	return value == "" || value == "0"
}

// parseSize accepts a byte count with an optional binary unit suffix: 4G,
// 512M, 1.5G, 512MiB, or a bare number of bytes.
func parseSize(value string, flag string) (uint64, error) {
	if unset(value) {
		return 0, nil
	}
	text := strings.TrimSpace(value)
	split := len(text)
	for split > 0 && !isNumeric(text[split-1]) {
		split--
	}
	digits := text[:split]
	// Normalize the unit so that B, MB, and MiB all reduce to the bare
	// multiplier letter (or the empty string for plain bytes).
	suffix := strings.ToUpper(text[split:])
	suffix = strings.TrimSuffix(suffix, "B")
	suffix = strings.TrimSuffix(suffix, "I")

	var multiplier uint64
	switch suffix {
	case "":
		multiplier = 1
	case "K":
		multiplier = 1 << 10
	case "M":
		multiplier = 1 << 20
	case "G":
		multiplier = 1 << 30
	case "T":
		multiplier = 1 << 40
	default:
		return 0, sizeError(flag, value)
	}

	number, err := strconv.ParseFloat(digits, 64)
	if err != nil || number < 0 {
		return 0, sizeError(flag, value)
	}
	bytes := number * float64(multiplier)
	if bytes < 1 {
		return 0, fmt.Errorf("invalid %s value %q; the limit must be at least one byte", flag, value)
	}
	return uint64(bytes), nil
}

func isNumeric(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.'
}

func sizeError(flag string, value string) error {
	return fmt.Errorf("invalid %s value %q; use a byte size such as 512M, 4G, or 2GiB", flag, value)
}

// parsePercent accepts a CPU share written as a percentage of one core. The %
// sign is required: a bare "2" is ambiguous between two cores and two percent,
// and silently picking one would be a surprising way to cap a build.
func parsePercent(value string, flag string) (uint64, error) {
	if unset(value) {
		return 0, nil
	}
	text, ok := strings.CutSuffix(strings.TrimSpace(value), "%")
	if !ok {
		return 0, fmt.Errorf("invalid %s value %q; use a percentage of one core such as 50%%, 100%%, or 400%% (the %% is required so a bare number is never read as a core count)", flag, value)
	}
	percent, err := strconv.ParseUint(strings.TrimSpace(text), 10, 32)
	if err != nil || percent == 0 {
		return 0, fmt.Errorf("invalid %s value %q; use a positive percentage of one core such as 50%% or 200%%", flag, value)
	}
	return percent, nil
}

func parseCount(value string, flag string) (uint64, error) {
	if unset(value) {
		return 0, nil
	}
	count, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil || count == 0 {
		return 0, fmt.Errorf("invalid %s value %q; use a positive whole number", flag, value)
	}
	return count, nil
}

// parseCPUTime accepts a Go duration, matching --timeout, but rounds up to
// whole seconds because RLIMIT_CPU counts in seconds.
func parseCPUTime(value string, flag string) (time.Duration, error) {
	if unset(value) {
		return 0, nil
	}
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid %s value %q; use a Go duration such as 30s, 2m, or 1h30m", flag, value)
	}
	if remainder := duration % time.Second; remainder != 0 {
		duration += time.Second - remainder
	}
	return duration, nil
}
