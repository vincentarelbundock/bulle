package policy

import (
	"testing"

	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestPlatformPoliciesAreSeparate(t *testing.T) {
	cfg := config.Config{
		Platform: config.PlatformSettings{
			Exec: config.PathSettings{ReadOnlyExec: []string{"/bin"}},
			Libs: config.PathSettings{
				ReadOnly:     []string{"/usr/share"},
				ReadOnlyExec: []string{"/lib"},
			},
		},
	}
	libs := platformLibsPolicy(cfg)
	tool := platformToolPolicy(cfg)

	if contains(libs.ReadOnlyExec, "/bin") {
		t.Fatalf("libs rox includes executable path: %#v", libs.ReadOnlyExec)
	}
	if !contains(tool.ReadOnlyExec, "/bin") {
		t.Fatalf("tool rox = %#v, want executable path", tool.ReadOnlyExec)
	}
	if !contains(tool.ReadOnlyExec, "/lib") {
		t.Fatalf("tool rox = %#v, want library path", tool.ReadOnlyExec)
	}
	if !contains(tool.ReadOnly, "/usr/share") {
		t.Fatalf("tool ro = %#v, want library read path", tool.ReadOnly)
	}
}

func TestDefaultConfigIncludesPlatformRuntimeRoots(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := platformToolPolicy(cfg)
	for _, want := range cfg.Platform.Exec.ReadOnlyExec {
		if !contains(tool.ReadOnlyExec, want) {
			t.Fatalf("tool rox missing executable root %q: %#v", want, tool.ReadOnlyExec)
		}
	}
	for _, want := range cfg.Platform.Libs.ReadOnlyExec {
		if !contains(tool.ReadOnlyExec, want) {
			t.Fatalf("tool rox missing library root %q: %#v", want, tool.ReadOnlyExec)
		}
	}
	for _, want := range cfg.Platform.Libs.ReadOnly {
		if !contains(tool.ReadOnly, want) {
			t.Fatalf("tool ro missing library read root %q: %#v", want, tool.ReadOnly)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
