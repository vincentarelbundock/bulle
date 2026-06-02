package policy

import "time"

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
}
