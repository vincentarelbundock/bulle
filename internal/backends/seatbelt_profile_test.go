package backends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestSeatbeltProfileMapsReadWriteAndExec(t *testing.T) {
	// Real directories rather than system paths: the profile builder asks the
	// filesystem whether an entry is a directory, so hardcoding /usr/share
	// tests the machine (it does not exist on NixOS) instead of the mapping.
	project := t.TempDir()
	shared := t.TempDir()
	execDir := t.TempDir()
	p := policy.Policy{
		ReadWrite:    []string{project},
		ReadOnly:     []string{shared},
		ReadOnlyExec: []string{execDir},
		MachLookup: []string{
			"com.apple.SystemConfiguration.DNSConfiguration",
			"com.apple.SystemConfiguration.configd",
			"com.apple.trustd.agent",
			"com.apple.system.opendirectoryd.libinfo",
		},
	}
	sbpl := BuildSeatbeltProfile(p)
	for _, want := range []string{
		"(deny default)",
		`(subpath "` + project + `")`,
		`(subpath "` + shared + `")`,
		"file-map-executable",
		`(global-name "com.apple.SystemConfiguration.DNSConfiguration")`,
		`(global-name "com.apple.SystemConfiguration.configd")`,
		`(global-name "com.apple.trustd.agent")`,
		`(global-name "com.apple.system.opendirectoryd.libinfo")`,
		"(allow network*)",
	} {
		if !strings.Contains(sbpl, want) {
			t.Fatalf("profile missing %q:\n%s", want, sbpl)
		}
	}
}

func TestSeatbeltProfileScopesProcessExecToExecRoots(t *testing.T) {
	execDir := t.TempDir()
	sbpl := BuildSeatbeltProfile(policy.Policy{ReadOnlyExec: []string{execDir}})
	if strings.Contains(sbpl, "(allow process-exec*)\n") {
		t.Fatalf("profile grants unconditional process-exec*:\n%s", sbpl)
	}
	if !strings.Contains(sbpl, `(allow process-exec*`) || !strings.Contains(sbpl, `(subpath "`+execDir+`")`) {
		t.Fatalf("profile should scope process-exec* to exec roots:\n%s", sbpl)
	}
}

func TestSeatbeltProfileDeniesProcessExecWithoutExecRoots(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{ReadOnly: []string{"/usr/share"}})
	if strings.Contains(sbpl, "process-exec") {
		t.Fatalf("profile should not grant process-exec when no exec roots are configured:\n%s", sbpl)
	}
}

func TestSeatbeltProfileOmitsNetworkWhenDenied(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{Network: policy.NetworkNone})
	if strings.Contains(sbpl, "(allow network*)") {
		t.Fatalf("profile allows network when network is none:\n%s", sbpl)
	}
}

func TestSeatbeltProfileAllowsAncestorDirectories(t *testing.T) {
	p := policy.Policy{
		ReadWrite:    []string{"/Users/vincent/Downloads/try"},
		ReadOnlyExec: []string{"/opt/homebrew/bin"},
	}
	sbpl := BuildSeatbeltProfile(p)
	if !strings.Contains(sbpl, "(allow file-read-data file-read-metadata\n  (literal \"/\")") {
		t.Fatalf("profile should allow only narrow ancestor directory reads:\n%s", sbpl)
	}
	if !strings.Contains(sbpl, "(allow file-read-metadata\n  (literal \"/opt\")") {
		t.Fatalf("profile should allow only metadata reads for non-root ancestors:\n%s", sbpl)
	}
	if strings.Contains(sbpl, "(allow file-read*\n  (literal \"/\")") {
		t.Fatalf("profile grants broad file-read* to ancestor directories:\n%s", sbpl)
	}
	if strings.Contains(sbpl, "(literal \"/\")\n  (literal \"/opt\")") {
		t.Fatalf("profile grants directory read-data to non-root ancestors:\n%s", sbpl)
	}
	for _, want := range []string{
		`(literal "/")`,
		`(literal "/Users")`,
		`(literal "/Users/vincent")`,
		`(literal "/Users/vincent/Downloads")`,
		`(literal "/opt")`,
		`(literal "/opt/homebrew")`,
	} {
		if !strings.Contains(sbpl, want) {
			t.Fatalf("profile missing ancestor %q:\n%s", want, sbpl)
		}
	}
}

func TestSeatbeltProfileAllowsExplicitPathsThemselves(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{
		ReadOnly:      []string{"/home"},
		ReadWrite:     []string{"/tmp/project"},
		ReadOnlyExec:  []string{"/usr/bin"},
		ReadWriteExec: []string{"/Users/vincent/.local/share/tool"},
	})
	for _, want := range []string{
		"(allow file-read*\n  (literal \"/home\")",
		"(allow file-read* file-write*\n  (literal \"/tmp/project\")",
		`(literal "/usr/bin")`,
		"(literal \"/Users/vincent/.local/share/tool\")",
	} {
		if !strings.Contains(sbpl, want) {
			t.Fatalf("profile should allow explicit path itself %q:\n%s", want, sbpl)
		}
	}
}

func TestSeatbeltProfileAllowsOnlyVarMetadataForMacOSResolver(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{})
	for _, want := range []string{`(literal "/var")`, `(literal "/tmp")`, `(literal "/private/tmp")`} {
		if !strings.Contains(sbpl, want) {
			t.Fatalf("profile should allow metadata for macOS resolver path %q:\n%s", want, sbpl)
		}
	}
	if strings.Contains(sbpl, "(subpath \"/var\")") {
		t.Fatalf("profile grants broad /var reads:\n%s", sbpl)
	}
	for _, path := range []string{`"/var"`, `"/tmp"`, `"/private/tmp"`} {
		if strings.Contains(sbpl, "(allow file-read-data file-read-metadata\n  (literal "+path+")") {
			t.Fatalf("profile grants read-data to metadata-only path %s:\n%s", path, sbpl)
		}
	}
	for _, want := range []string{`(subpath "/tmp")`, `(subpath "/private/tmp")`} {
		if !strings.Contains(sbpl, want) {
			t.Fatalf("profile should allow metadata under macOS temp path %q:\n%s", want, sbpl)
		}
	}
}

func TestSeatbeltProfileAllowsOnlyRequiredDeviceFiles(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{})
	for _, want := range []string{
		`(literal "/dev")`,
		`(literal "/dev/tty")`,
		`(literal "/dev/null")`,
		`(literal "/dev/urandom")`,
	} {
		if !strings.Contains(sbpl, want) {
			t.Fatalf("profile missing device file %q:\n%s", want, sbpl)
		}
	}
	if strings.Contains(sbpl, `^/dev/ttys[0-9]+$`) {
		t.Fatalf("profile should not grant broad terminal regex access:\n%s", sbpl)
	}
	if !strings.Contains(sbpl, "(allow file-ioctl\n  (literal \"/dev/tty\")\n  (literal \"/dev/null\")") {
		t.Fatalf("profile should allow terminal ioctl on narrow device files:\n%s", sbpl)
	}
	if strings.Contains(sbpl, `(subpath "/dev")`) {
		t.Fatalf("profile grants broad /dev access:\n%s", sbpl)
	}
}

func TestSeatbeltProfileAllowsSymlinkComponentsForPathResolution(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	framework := filepath.Join(root, "Framework.framework")
	versions := filepath.Join(framework, "Versions")
	realBin := filepath.Join(versions, "1.0", "Resources", "bin")
	for _, path := range []string{binDir, realBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(binDir, "tool")
	resourceLink := filepath.Join(framework, "Resources")
	currentLink := filepath.Join(versions, "Current")
	target := filepath.Join(realBin, "tool")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("1.0", currentLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("Versions", "Current", "Resources"), resourceLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(framework, "Resources", "bin", "tool"), launcher); err != nil {
		t.Fatal(err)
	}

	sbpl := BuildSeatbeltProfile(policy.Policy{ReadOnlyExec: []string{launcher, target}})
	for _, want := range []string{resourceLink, currentLink} {
		if !strings.Contains(sbpl, `(literal "`+want+`")`) {
			t.Fatalf("profile missing symlink component %q:\n%s", want, sbpl)
		}
	}
}

func TestSeatbeltProfileAllowsSymlinkComponentsAfterAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	aliasRoot := filepath.Join(root, "alias")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(aliasRoot, "bin")
	framework := filepath.Join(aliasRoot, "Framework.framework")
	versions := filepath.Join(framework, "Versions")
	realBin := filepath.Join(versions, "1.0", "Resources", "bin")
	for _, path := range []string{binDir, realBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(binDir, "tool")
	resourceLink := filepath.Join(framework, "Resources")
	currentLink := filepath.Join(versions, "Current")
	target := filepath.Join(realBin, "tool")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("1.0", currentLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("Versions", "Current", "Resources"), resourceLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(framework, "Resources", "bin", "tool"), launcher); err != nil {
		t.Fatal(err)
	}

	sbpl := BuildSeatbeltProfile(policy.Policy{ReadOnlyExec: []string{launcher, target}})
	for _, want := range []string{resourceLink, currentLink} {
		if !strings.Contains(sbpl, `(literal "`+want+`")`) {
			t.Fatalf("profile missing symlink component %q:\n%s", want, sbpl)
		}
	}
}

func TestSeatbeltProfileAllowsConfiguredMachLookups(t *testing.T) {
	withoutMachLookup := BuildSeatbeltProfile(policy.Policy{})
	if strings.Contains(withoutMachLookup, "com.apple.SecurityServer") {
		t.Fatalf("profile allows SecurityServer without explicit mach_lookup:\n%s", withoutMachLookup)
	}

	withMachLookup := BuildSeatbeltProfile(policy.Policy{MachLookup: []string{
		"com.apple.SecurityServer",
		"com.apple.securityd",
	}})
	for _, want := range []string{
		`(global-name "com.apple.SecurityServer")`,
		`(global-name "com.apple.securityd")`,
	} {
		if !strings.Contains(withMachLookup, want) {
			t.Fatalf("profile missing mach lookup %q:\n%s", want, withMachLookup)
		}
	}
}

func TestSeatbeltProfileDoesNotGrantSubpathForWritableFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sbpl := BuildSeatbeltProfile(policy.Policy{ReadWrite: []string{file}})
	if strings.Contains(sbpl, `(subpath "`+file+`")`) {
		t.Fatalf("profile grants subpath access for file path:\n%s", sbpl)
	}
	if !strings.Contains(sbpl, `(literal "`+file+`")`) {
		t.Fatalf("profile missing literal access for file path:\n%s", sbpl)
	}
}

func TestSeatbeltProfileAllowsAtomicScratchFilesForWritableFiles(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{
		ReadWrite: []string{`/Users/vincent/.claude.json`},
	})
	if !strings.Contains(sbpl, `(regex "^/Users/vincent/\\.claude\\.json\\.(lock|tmp\\.[^/]*)$")`) {
		t.Fatalf("profile missing atomic scratch file allowance:\n%s", sbpl)
	}
}

func TestSeatbeltProfileDoesNotAddAtomicScratchFilesForWritableDirectories(t *testing.T) {
	sbpl := BuildSeatbeltProfile(policy.Policy{
		ReadWrite: []string{t.TempDir()},
	})
	if strings.Contains(sbpl, `(lock|tmp\\..*)`) {
		t.Fatalf("profile should not add atomic scratch regex for directories:\n%s", sbpl)
	}
}
