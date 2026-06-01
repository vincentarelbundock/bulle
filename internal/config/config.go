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
)

type Config struct {
	Settings

	Profiles        map[string]Profile         `toml:"profiles"`
	ProfileMetadata map[string]ProfileMetadata `toml:"-"`
	Platform        PlatformSettings           `toml:"platform"`
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
