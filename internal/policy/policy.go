package policy

type BackendName string

const (
	BackendLinuxLandlock BackendName = "linux-landlock"
	BackendMacOSSeatbelt BackendName = "macos-seatbelt"
)

type Policy struct {
	ProjectPath string
	Command     []string

	ReadOnly      []string
	ReadOnlyExec  []string
	ReadWrite     []string
	ReadWriteExec []string

	Env map[string]string

	AddExec bool
	AddLibs bool
	Backend BackendName

	AllowKeychain bool
}

type View struct {
	Backend       BackendName `json:"backend"`
	ProjectPath   string      `json:"workspace_path"`
	Command       []string    `json:"command"`
	ReadOnly      []string    `json:"ro"`
	ReadOnlyExec  []string    `json:"rox"`
	ReadWrite     []string    `json:"rw"`
	ReadWriteExec []string    `json:"rwx"`
	EnvKeys       []string    `json:"env_keys"`
	AddExec       bool        `json:"add_exec"`
	AddLibs       bool        `json:"add_libs"`
	AllowKeychain bool        `json:"allow_keychain"`
}
