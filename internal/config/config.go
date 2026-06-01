package config

import (
	"embed"
	"os"
	"runtime"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Settings

	DefaultProfile string             `toml:"default_profile"`
	Vars           map[string]string  `toml:"vars"`
	Profiles       map[string]Profile `toml:"profiles"`
	Platform       PlatformSettings   `toml:"platform"`
}

type PlatformSettings struct {
	Exec PathSettings `toml:"exec"`
	Libs PathSettings `toml:"libs"`
}

type Profile struct {
	Settings

	Inherits string `toml:"inherits"`
}

type Settings struct {
	DefaultApp string `toml:"default_app"`
	Network    string `toml:"network"`

	PathSettings `toml:",inline"`

	Env []string `toml:"env"`

	ReplaceEnv bool `toml:"replace_env"`

	AddExec bool `toml:"add_exec"`
	AddLibs bool `toml:"add_libs"`

	AllowKeychain *bool `toml:"allow_keychain"`
}

type PathSettings struct {
	ReadOnly      []string `toml:"ro"`
	ReadOnlyExec  []string `toml:"rox"`
	ReadWrite     []string `toml:"rw"`
	ReadWriteExec []string `toml:"rwx"`

	ReplaceReadOnly      bool `toml:"replace_ro"`
	ReplaceReadOnlyExec  bool `toml:"replace_rox"`
	ReplaceReadWrite     bool `toml:"replace_rw"`
	ReplaceReadWriteExec bool `toml:"replace_rwx"`
}

func LoadBytes(data []byte) (Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	if cfg.Vars == nil {
		cfg.Vars = map[string]string{}
	}
	return cfg, nil
}

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return LoadBytes(data)
}

//go:embed defaults.toml defaults_darwin.toml defaults_linux.toml
var defaultConfigFS embed.FS

func DefaultConfig() Config {
	cfg, err := LoadDefaultConfig()
	if err != nil {
		panic(err)
	}
	return cfg
}

func LoadDefaultConfig() (Config, error) {
	data, err := defaultConfigFS.ReadFile("defaults.toml")
	if err != nil {
		return Config{}, err
	}
	base, err := LoadBytes(data)
	if err != nil {
		return Config{}, err
	}
	platformData, err := defaultConfigFS.ReadFile("defaults_" + runtime.GOOS + ".toml")
	if err != nil {
		return base, nil
	}
	platform, err := LoadBytes(platformData)
	if err != nil {
		return Config{}, err
	}
	return MergeConfigs(base, platform), nil
}

func (c Config) TopLevelProfile() Profile {
	return Profile{
		Settings: cloneSettings(c.Settings),
	}
}
