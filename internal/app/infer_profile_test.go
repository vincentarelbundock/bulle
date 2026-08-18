package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestRunNeverInfersPrivilegedProfileFromCommandName(t *testing.T) {
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
	if strings.Contains(stderr.String(), `using profile`) {
		t.Fatalf("stderr announces unsafe profile inference: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), `bulle profile "default" permissions:`) {
		t.Fatalf("stdout summary is not for the unprivileged default profile: %s", stdout.String())
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

func TestRunDoesNotInspectMatchingProfiles(t *testing.T) {
	configRoot, script := writeInferProfileFixture(t, map[string]string{
		"codexish":  "default_app = \"%SCRIPT%\"\nrox = [\"%BIN%\", \"/bin\", \"/usr/bin\", \"/nix/store\"]\n",
		"codexish2": "default_app = \"%SCRIPT%\"\nrox = [\"%BIN%\", \"/bin\", \"/usr/bin\", \"/nix/store\"]\n",
	})
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"bulle", "show", "--config", configRoot, "--", script,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "codexish") {
		t.Fatalf("explicit command caused profile-name matching: %s", stderr.String())
	}
}
