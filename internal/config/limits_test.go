package config

import "testing"

const limitsConfig = `
[defaults.limits]
nofile = "4096"

[defaults.linux.limits]
memory = "4G"

[defaults.macos.limits]
fsize = "100M"
`

func TestDefaultsLimitSpecLayersThePlatformBlockOverTheSharedOne(t *testing.T) {
	cfg, err := LoadBytes([]byte(limitsConfig))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}

	linux := cfg.Defaults.LimitSpec("linux")
	if linux.NoFile != "4096" {
		t.Errorf("linux nofile = %q, want the shared value", linux.NoFile)
	}
	if linux.Memory != "4G" {
		t.Errorf("linux memory = %q, want the linux value", linux.Memory)
	}
	// The macOS-only limit must not leak onto Linux.
	if linux.FSize != "" {
		t.Errorf("linux fsize = %q, want it unset", linux.FSize)
	}

	// A limit scoped to Linux is simply not requested on macOS, which is what
	// keeps a shared configuration from warning on the platform it never
	// targeted.
	macos := cfg.Defaults.LimitSpec("darwin")
	if macos.Memory != "" {
		t.Errorf("macos memory = %q, want it unset", macos.Memory)
	}
	if macos.FSize != "100M" {
		t.Errorf("macos fsize = %q, want the macos value", macos.FSize)
	}
	if macos.NoFile != "4096" {
		t.Errorf("macos nofile = %q, want the shared value", macos.NoFile)
	}
}

func TestPlatformLimitOverridesTheSharedLimit(t *testing.T) {
	cfg, err := LoadBytes([]byte(`
[defaults.limits]
memory = "1G"

[defaults.linux.limits]
memory = "8G"
`))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if got := cfg.Defaults.LimitSpec("linux").Memory; got != "8G" {
		t.Errorf("linux memory = %q, want the platform block to win", got)
	}
	if got := cfg.Defaults.LimitSpec("darwin").Memory; got != "1G" {
		t.Errorf("macos memory = %q, want the shared value", got)
	}
}

func TestMergeDefaultsLayersLimitsPerKey(t *testing.T) {
	parent := DefaultsSettings{
		Limits: LimitSettings{Memory: "1G", NProc: "100"},
		Linux:  DefaultsPlatformSettings{Limits: LimitSettings{CPU: "100%"}},
	}
	child := DefaultsSettings{
		Limits: LimitSettings{Memory: "4G"},
	}
	merged := mergeDefaults(parent, child)
	if merged.Limits.Memory != "4G" {
		t.Errorf("memory = %q, want the child value", merged.Limits.Memory)
	}
	if merged.Limits.NProc != "100" {
		t.Errorf("nproc = %q, want the parent value to survive", merged.Limits.NProc)
	}
	if merged.Linux.Limits.CPU != "100%" {
		t.Errorf("linux cpu = %q, want the parent platform block to survive", merged.Linux.Limits.CPU)
	}
}

// strict_limits is a pointer so that an explicit false in a lower-precedence
// file is not indistinguishable from an absent key.
func TestMergeDefaultsKeepsAnExplicitStrictLimitsFalse(t *testing.T) {
	no := false
	yes := true
	merged := mergeDefaults(DefaultsSettings{StrictLimits: &yes}, DefaultsSettings{StrictLimits: &no})
	if merged.StrictLimits == nil || *merged.StrictLimits {
		t.Fatalf("StrictLimits = %v, want the child's explicit false", merged.StrictLimits)
	}
	inherited := mergeDefaults(DefaultsSettings{StrictLimits: &yes}, DefaultsSettings{})
	if inherited.StrictLimits == nil || !*inherited.StrictLimits {
		t.Fatalf("StrictLimits = %v, want the parent value when the child is silent", inherited.StrictLimits)
	}
}
