package backends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestSeatbeltProfileMapsReadWriteAndExec(t *testing.T) {
	project := t.TempDir()
	p := policy.Policy{
		ReadWrite:    []string{project},
		ReadOnly:     []string{"/usr/share"},
		ReadOnlyExec: []string{"/usr/bin"},
	}
	sbpl := BuildSeatbeltProfile(p)
	for _, want := range []string{
		"(deny default)",
		`(subpath "` + project + `")`,
		`(subpath "/usr/share")`,
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

func TestSeatbeltProfileAllowsKeychainOnlyWhenRequested(t *testing.T) {
	withoutKeychain := BuildSeatbeltProfile(policy.Policy{})
	for _, name := range []string{"com.apple.SecurityServer", "com.apple.securityd"} {
		if strings.Contains(withoutKeychain, name) {
			t.Fatalf("profile allows %s without keychain opt-in:\n%s", name, withoutKeychain)
		}
	}

	withKeychain := BuildSeatbeltProfile(policy.Policy{AllowKeychain: true})
	for _, want := range []string{
		`(global-name "com.apple.SecurityServer")`,
		`(global-name "com.apple.securityd")`,
		`(global-name "com.apple.securityd.xpc")`,
	} {
		if !strings.Contains(withKeychain, want) {
			t.Fatalf("profile missing keychain permission %q:\n%s", want, withKeychain)
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
