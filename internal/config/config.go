package config

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/vincentarelbundock/bulle/internal/limits"
)

type Config struct {
	Settings

	Profiles        map[string]Profile         `toml:"profiles"`
	ProfileMetadata map[string]ProfileMetadata `toml:"-"`
	Platform        PlatformSettings           `toml:"platform"`
	Vars            map[string]string          `toml:"vars"`
	Defaults        DefaultsSettings           `toml:"defaults"`
	Scratch         ScratchSettings            `toml:"scratch"`
}

// ScratchSettings is the [scratch] block of the user configuration.
type ScratchSettings struct {
	// Dir overrides where scratch clones are created; useful when the default
	// state directory is on a different filesystem than the repositories, which
	// would defeat object hardlinking.
	Dir string `toml:"dir"`
}

// DefaultsSettings is the [defaults] block of the user configuration: values
// applied when the corresponding flag is absent, so bare `bulle` does the
// usual thing. Explicit flags always win, and --no-defaults ignores the block.
type DefaultsSettings struct {
	Profile string   `toml:"profile"`
	Timeout string   `toml:"timeout"`
	Env     []string `toml:"env"`

	// StrictLimits turns an unenforceable resource limit from a warning into a
	// refusal to run. It is a pointer so that an explicit false in the user
	// configuration is distinguishable from an absent key when merging.
	StrictLimits *bool `toml:"strict_limits"`

	// Limits applies on every platform. The per-platform blocks below apply
	// only where they match, which is how a limit that one platform cannot
	// enforce is requested without warning on the other.
	Limits LimitSettings `toml:"limits"`

	MacOS DefaultsPlatformSettings `toml:"macos"`
	Linux DefaultsPlatformSettings `toml:"linux"`

	PathSettings `toml:",inline"`
}

// DefaultsPlatformSettings is a [defaults.macos] or [defaults.linux] block:
// the parts of [defaults] that are worth scoping to one platform.
type DefaultsPlatformSettings struct {
	Limits LimitSettings `toml:"limits"`
}

// LimitSettings is a [limits] block. Values are kept as written and parsed
// once the flags and the configuration have been merged, so an invalid value
// is reported against whichever source actually won.
type LimitSettings struct {
	Memory  string `toml:"memory"`
	CPU     string `toml:"cpu"`
	NProc   string `toml:"nproc"`
	NoFile  string `toml:"nofile"`
	FSize   string `toml:"fsize"`
	CPUTime string `toml:"cpu_time"`
}

// Spec converts the configuration block into the limits package's unparsed
// form.
func (l LimitSettings) Spec() limits.Spec {
	return limits.Spec{
		Memory:  l.Memory,
		CPU:     l.CPU,
		NProc:   l.NProc,
		NoFile:  l.NoFile,
		FSize:   l.FSize,
		CPUTime: l.CPUTime,
	}
}

// LimitSpec returns the limits requested by [defaults] for goos: the unscoped
// block with the matching platform block layered on top. A limit written only
// under a non-matching platform is absent from the result, so it is never
// requested and never warned about.
func (d DefaultsSettings) LimitSpec(goos string) limits.Spec {
	spec := d.Limits.Spec()
	switch PlatformKey(goos) {
	case "macos":
		return spec.Merge(d.MacOS.Limits.Spec())
	case "linux":
		return spec.Merge(d.Linux.Limits.Spec())
	}
	return spec
}

type ProfileMetadata struct {
	Title       string `toml:"title"`
	Description string `toml:"description"`
}

type ProfileFile struct {
	ProfileMetadata
	Profile
}

type PlatformSettings struct {
	Exec       PathSettings `toml:"exec"`
	Libs       PathSettings `toml:"libs"`
	MachLookup []string     `toml:"mach_lookup"`

	MacOS PlatformPathSettings `toml:"macos"`
	Linux PlatformPathSettings `toml:"linux"`
}

type PlatformPathSettings struct {
	Exec       PathSettings `toml:"exec"`
	Libs       PathSettings `toml:"libs"`
	MachLookup []string     `toml:"mach_lookup"`
}

type Profile struct {
	Settings

	Inherits InheritList `toml:"inherits"`
	MacOS    Settings    `toml:"macos"`
	Linux    Settings    `toml:"linux"`
}

type Settings struct {
	DefaultApp string `toml:"default_app"`

	PathSettings `toml:",inline"`

	Env            []string `toml:"env"`
	Allow          []string `toml:"allow"`
	Deny           []string `toml:"deny"`
	MachLookup     []string `toml:"mach_lookup"`
	DenyMachLookup []string `toml:"deny_mach_lookup"`

	AddExec *bool `toml:"add_exec"`
	AddLibs *bool `toml:"add_libs"`
}

type PathSettings struct {
	ReadOnly      []string `toml:"ro"`
	ReadOnlyExec  []string `toml:"rox"`
	ReadWrite     []string `toml:"rw"`
	ReadWriteExec []string `toml:"rwx"`
}

type InheritList struct {
	Names []string
	Set   bool
}

func Inherits(names ...string) InheritList {
	return InheritList{Names: append([]string{}, names...), Set: true}
}

func (i *InheritList) UnmarshalTOML(value *unstable.Node) error {
	i.Set = true
	switch value.Kind {
	case unstable.String:
		i.Names = []string{string(value.Data)}
	case unstable.Array:
		i.Names = nil
		children := value.Children()
		for children.Next() {
			child := children.Node()
			if child.Kind != unstable.String {
				return fmt.Errorf("inherits entries must be strings")
			}
			i.Names = append(i.Names, string(child.Data))
		}
	default:
		return fmt.Errorf("inherits must be a string or list of strings")
	}
	return nil
}

func LoadBytes(data []byte) (Config, error) {
	cfg, err := decodeBytes(data)
	if err != nil {
		return Config{}, err
	}
	return withConfigDefaults(cfg), nil
}

func decodeBytes(data []byte) (Config, error) {
	var cfg Config
	decoder := toml.NewDecoder(bytes.NewReader(data)).EnableUnmarshalerInterface().DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func decodeProfileFile(data []byte) (ProfileFile, error) {
	var profileFile ProfileFile
	decoder := toml.NewDecoder(bytes.NewReader(data)).EnableUnmarshalerInterface().DisallowUnknownFields()
	if err := decoder.Decode(&profileFile); err != nil {
		return ProfileFile{}, err
	}
	return profileFile, nil
}

func withConfigDefaults(cfg Config) Config {
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	if cfg.ProfileMetadata == nil {
		cfg.ProfileMetadata = map[string]ProfileMetadata{}
	}
	return cfg
}

func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return LoadBytes(data)
}

func LoadProfileFile(path string) (string, Profile, ProfileMetadata, error) {
	name, profile, metadata, err := loadProfileFragment(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		return "", Profile{}, ProfileMetadata{}, err
	}
	return name, profile, metadata, nil
}

func LoadProfileDirectory(path string) (Config, error) {
	cfg := withConfigDefaults(Config{})
	if err := loadProfileFragmentsInto(&cfg, os.DirFS(path), ".", "profile directory"); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

//go:embed defaults.toml profiles/*.toml
var defaultConfigFS embed.FS

func DefaultConfig() Config {
	cfg, err := LoadDefaultConfig()
	if err != nil {
		panic(err)
	}
	return cfg
}

func LoadDefaultConfig() (Config, error) {
	return loadDefaultConfigFromFS(defaultConfigFS)
}

func loadDefaultConfigFromFS(fsys fs.FS) (Config, error) {
	data, err := fs.ReadFile(fsys, "defaults.toml")
	if err != nil {
		return Config{}, err
	}
	cfg, err := decodeBytes(data)
	if err != nil {
		return Config{}, fmt.Errorf("load defaults.toml: %w", err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	if cfg.ProfileMetadata == nil {
		cfg.ProfileMetadata = map[string]ProfileMetadata{}
	}

	if err := loadProfileFragmentsInto(&cfg, fsys, "profiles", "embedded profile"); err != nil {
		return Config{}, err
	}
	return withConfigDefaults(cfg), nil
}

func loadProfileFragmentsInto(cfg *Config, fsys fs.FS, dir string, source string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		profilePath := entry.Name()
		if dir != "." {
			profilePath = dir + "/" + entry.Name()
		}
		if entry.IsDir() {
			return fmt.Errorf("%s %s is a directory", source, profilePath)
		}
		name, profile, metadata, err := loadProfileFragment(fsys, profilePath)
		if err != nil {
			return err
		}
		if _, exists := cfg.Profiles[name]; exists {
			return fmt.Errorf("%s %s defines duplicate profile %q", source, profilePath, name)
		}
		cfg.Profiles[name] = profile
		cfg.ProfileMetadata[name] = metadata
	}
	return nil
}

func loadProfileFragment(fsys fs.FS, profilePath string) (string, Profile, ProfileMetadata, error) {
	base := pathpkg.Base(profilePath)
	wantName, ok := strings.CutSuffix(base, ".toml")
	if !ok || wantName == "" || strings.Contains(wantName, "/") {
		return "", Profile{}, ProfileMetadata{}, fmt.Errorf("profile %s must be a <name>.toml file", profilePath)
	}
	data, err := fs.ReadFile(fsys, profilePath)
	if err != nil {
		return "", Profile{}, ProfileMetadata{}, err
	}
	profileFile, err := decodeProfileFile(data)
	if err != nil {
		return "", Profile{}, ProfileMetadata{}, fmt.Errorf("load %s: %w", profilePath, err)
	}
	return wantName, profileFile.Profile, profileFile.ProfileMetadata, nil
}

func PlatformKey(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

func currentPlatformKey() string {
	return PlatformKey(runtime.GOOS)
}

func (c Config) TopLevelProfile() Profile {
	return Profile{
		Settings: cloneSettings(c.Settings),
	}
}
