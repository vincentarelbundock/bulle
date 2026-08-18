package policy

import (
	"time"

	"github.com/vincentarelbundock/bulle/internal/limits"
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

type BackendName string
type NetworkMode string

const (
	BackendLinuxLandlock BackendName = "linux-landlock"
	BackendMacOSSeatbelt BackendName = "macos-seatbelt"

	NetworkFull NetworkMode = "full"
	NetworkNone NetworkMode = "none"
)

type Policy struct {
	ProjectPath string
	Command     []string
	Timeout     time.Duration

	// Limits are the requested resource constraints. The cgroup-backed ones
	// are applied by the supervisor around the sandboxed child; the rlimit
	// ones travel with the policy and are applied inside it.
	Limits limits.Limits

	ReadOnly      []string
	ReadOnlyExec  []string
	ReadWrite     []string
	ReadWriteExec []string

	Env map[string]string

	AddExec bool
	AddLibs bool
	Backend BackendName

	MachLookup []string
	Network    NetworkMode

	// ShimDir is the per-run directory of symlinks created for which:/pkg:
	// entries, or empty. The caller removes it after the run.
	ShimDir string
	// Trace records the per-entry resolution outcomes for --policy output.
	Trace []bpaths.Trace
	// Notes are non-fatal warnings produced while preparing the policy (for
	// example, a truncated add_libs scan). They are printed to stderr before
	// the sandboxed command runs.
	Notes []string

	// AuditMarker is an internal file the Linux child deliberately attempts to
	// read after entering its Landlock domain. Its denial identifies that exact
	// domain in the global kernel audit log. It is never granted to the target.
	AuditMarker string
}

type View struct {
	Backend       BackendName `json:"backend"`
	ProjectPath   string      `json:"workspace_path"`
	Command       []string    `json:"command"`
	Timeout       string      `json:"timeout,omitempty"`
	ReadOnly      []string    `json:"ro"`
	ReadOnlyExec  []string    `json:"rox"`
	ReadWrite     []string    `json:"rw"`
	ReadWriteExec []string    `json:"rwx"`
	EnvKeys       []string    `json:"env_keys"`
	AddExec       bool        `json:"add_exec"`
	AddLibs       bool        `json:"add_libs"`
	MachLookup    []string    `json:"mach_lookup"`
	Network       NetworkMode `json:"network"`
	// Limits reports each requested resource limit together with what enforces
	// it here, so "am I actually capped?" is answerable from bulle policy
	// rather than inferred from the platform.
	Limits []limits.Status `json:"limits,omitempty"`
}
