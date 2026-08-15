package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestProfilesMatchingCommand(t *testing.T) {
	global := config.Config{Profiles: map[string]config.Profile{
		"default": {Settings: config.Settings{DefaultApp: "codex"}},
		"codex":   {Settings: config.Settings{DefaultApp: "/usr/bin/codex --yolo"}},
		"claude":  {Settings: config.Settings{DefaultApp: "claude"}},
		"tool":    {},
	}}
	cases := []struct {
		command string
		want    []string
	}{
		{"codex", []string{"codex"}},
		{"/opt/bin/codex", []string{"codex"}},
		{"claude", []string{"claude"}},
		{"vim", nil},
	}
	for _, c := range cases {
		if got := profilesMatchingCommand(global, c.command); !reflect.DeepEqual(got, c.want) {
			t.Errorf("profilesMatchingCommand(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

// writeInferProfileFixture creates an executable script and a config root
// whose profiles/ directory holds the given TOML files. Each content string
// may reference the script path via the %SCRIPT% and its directory via the
// %BIN% placeholder.
func writeInferProfileFixture(t *testing.T, profiles map[string]string) (configRoot string, script string) {
	t.Helper()
	binDir := t.TempDir()
	script = filepath.Join(binDir, "codexish")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configRoot = t.TempDir()
	profileDir := filepath.Join(configRoot, "profiles")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range profiles {
		content = strings.ReplaceAll(content, "%SCRIPT%", script)
		content = strings.ReplaceAll(content, "%BIN%", binDir)
		if err := os.WriteFile(filepath.Join(profileDir, name+".toml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configRoot, script
}

func TestRunInfersProfileFromCommand(t *testing.T) {
	configRoot, script := writeInferProfileFixture(t, map[string]string{
		"codexish": "default_app = \"%SCRIPT%\"\nrox = [\"%BIN%\", \"/bin\", \"/usr/bin\", \"/nix/store\"]\n",
	})
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"bulle", "show", "--config", configRoot, "--", script,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `using profile "codexish"`) {
		t.Fatalf("stderr missing inference announcement: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `bulle profile "codexish" permissions:`) {
		t.Fatalf("stdout summary not for inferred profile: %s", stdout.String())
	}
}

func TestRunDoesNotInferProfileWhenExplicitProfileGiven(t *testing.T) {
	configRoot, script := writeInferProfileFixture(t, map[string]string{
		"codexish": "default_app = \"%SCRIPT%\"\nrox = [\"%BIN%\", \"/bin\", \"/usr/bin\", \"/nix/store\"]\n",
	})
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"bulle", "show", "--config", configRoot, "default", t.TempDir(), "--", script,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "using profile") {
		t.Fatalf("inference fired despite an explicit profile: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `bulle profile "default" permissions:`) {
		t.Fatalf("stdout summary not for the explicit profile: %s", stdout.String())
	}
}

func TestRunRefusesToInferAmbiguousProfiles(t *testing.T) {
	configRoot, script := writeInferProfileFixture(t, map[string]string{
		"codexish":  "default_app = \"%SCRIPT%\"\nrox = [\"%BIN%\", \"/bin\", \"/usr/bin\", \"/nix/store\"]\n",
		"codexish2": "default_app = \"%SCRIPT%\"\nrox = [\"%BIN%\", \"/bin\", \"/usr/bin\", \"/nix/store\"]\n",
	})
	var stdout, stderr bytes.Buffer
	// Ambiguity is announced and the run proceeds under the default profile.
	code := Run([]string{
		"bulle", "show", "--config", configRoot, "--", script,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "codexish, codexish2") || !strings.Contains(stderr.String(), "name one before --") {
		t.Fatalf("stderr missing ambiguity note: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "using profile \"") {
		t.Fatalf("inference chose a profile despite ambiguity: %s", stderr.String())
	}
}
