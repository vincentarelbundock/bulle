package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestShouldPrintProfileSummary(t *testing.T) {
	tests := []struct {
		name string
		opts cli.Options
		want bool
	}{
		{
			name: "explicit profile normal run",
			opts: cli.Options{Flags: cli.Flags{Profile: "tool"}},
			want: true,
		},
		{
			name: "explicit profile policy output",
			opts: cli.Options{Flags: cli.Flags{Profile: "tool"}, Policy: true},
			want: false,
		},
		{
			name: "no explicit profile",
			opts: cli.Options{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPrintProfileSummary(tt.opts); got != tt.want {
				t.Fatalf("shouldPrintProfileSummary(%#v) = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}
}

func TestWriteProfilePermissionSummaryFormatsPolicy(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:       policy.BackendMacOSSeatbelt,
		ProjectPath:   "/tmp/project",
		Command:       []string{"echo", "hi"},
		ReadOnly:      []string{"/readonly"},
		ReadOnlyExec:  []string{"/bin"},
		ReadWrite:     []string{"/write"},
		ReadWriteExec: []string{"/exec"},
		Env: map[string]string{
			"OPENAI_API_KEY": "openai-secret-value",
			"PATH":           "path-secret-value",
		},
		AddExec:    true,
		AddLibs:    false,
		MachLookup: []string{"com.apple.SecurityServer"},
		Network:    policy.NetworkNone,
	}

	writeProfilePermissionSummary("agent", p, &stderr, false)

	got := stderr.String()
	assertContains(t, got, `bulle profile "agent" permissions:`)
	assertContains(t, got, "  backend: macos-seatbelt\n")
	assertContains(t, got, "  command: echo hi\n")
	assertContains(t, got, "  workspace: /tmp/project\n")
	assertContains(t, got, "  filesystem:\n")
	assertContains(t, got, "    ro: /readonly\n")
	assertContains(t, got, "    rox: /bin\n")
	assertContains(t, got, "    rw: /write\n")
	assertContains(t, got, "    rwx: /exec\n")
	assertContains(t, got, "  environment: OPENAI_API_KEY, PATH\n")
	assertContains(t, got, "  network: none\n")
	assertContains(t, got, "  mach_lookup: com.apple.SecurityServer\n")
	// With a command, add_exec/add_libs results are already in the lists.
	assertNotContains(t, got, "add_exec")
	assertNotContains(t, got, "add_libs")
	assertNotContains(t, got, "openai-secret-value")
	assertNotContains(t, got, "path-secret-value")
}

func TestWriteProfilePermissionSummaryQuotesAmbiguousCommandArgs(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendLinuxLandlock,
		ProjectPath: "/tmp/project",
		Command:     []string{"cmd", "two words", `quote"arg`, "semi;colon"},
		Env:         map[string]string{},
	}

	writeProfilePermissionSummary("agent", p, &stderr, false)

	got := stderr.String()
	assertContains(t, got, `  command: cmd "two words" "quote\"arg" "semi;colon"`+"\n")
}

func TestWriteProfilePermissionSummaryIncludesTimeoutWhenSet(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendLinuxLandlock,
		ProjectPath: "/tmp/project",
		Command:     []string{"sleep", "60"},
		Env:         map[string]string{},
		Timeout:     30 * time.Second,
	}

	writeProfilePermissionSummary("agent", p, &stderr, false)

	assertContains(t, stderr.String(), "  timeout: 30s\n")
}

func TestWriteProfilePermissionSummaryCompactsFilesystemPaths(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendMacOSSeatbelt,
		ProjectPath: "/Users/vincent/repos/bulle",
		Command:     []string{"pi"},
		ReadOnly: []string{
			"/usr/local/etc",
			"/usr/local/share",
			"/opt/homebrew/etc",
			"/opt/homebrew/share",
		},
		ReadOnlyExec: []string{
			"/Users/vincent/.local/bin",
			"/Users/vincent/.bin",
			"/opt/homebrew/bin",
			"/opt/homebrew/lib",
			"/opt/homebrew/Cellar",
			"/opt/homebrew/opt",
		},
		ReadWrite: []string{
			"/Users/vincent/repos/bulle",
			"/var/folders/example/T/bulle/tmp",
			"/private/var/folders/example/T/bulle/tmp",
		},
		ReadWriteExec: []string{"/Users/vincent/.pi"},
		Env: map[string]string{
			"HOME": "/Users/vincent",
			"TMP":  "/var/folders/example/T",
		},
	}

	writeProfilePermissionSummary("pi", p, &stderr, false)

	got := stderr.String()
	assertContains(t, got, "    ro: /usr/local/{etc,share}, /opt/homebrew/{etc,share}\n")
	assertContains(t, got, "    rox: ~/.local/bin, ~/.bin, /opt/homebrew/{bin,lib,Cellar,opt}\n")
	assertContains(t, got, "    rw: $WORKSPACE, $TMP/bulle/tmp (+ /private alias)\n")
	assertContains(t, got, "    rwx: ~/.pi\n")
	assertNotContains(t, got, "      - /usr/local/etc")
	assertNotContains(t, got, "      - /Users/vincent/.pi")
}

func TestWriteProfilePermissionSummaryShowsNoneForEmptyGroups(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendLinuxLandlock,
		ProjectPath: "/tmp/project",
		Env:         map[string]string{},
	}

	writeProfilePermissionSummary("strict", p, &stderr, false)

	got := stderr.String()
	assertContains(t, got, "  command: none\n")
	assertContains(t, got, "    ro: none\n")
	assertContains(t, got, "    rox: none\n")
	assertContains(t, got, "    rw: none\n")
	assertContains(t, got, "    rwx: none\n")
	assertContains(t, got, "  environment: none\n")
	assertContains(t, got, "  network: full\n")
	assertNotContains(t, got, "mach_lookup")
}

func TestWriteProfilePermissionSummaryRecognizesUserSpecificRuntimeTmp(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendLinuxLandlock,
		ProjectPath: "/workspace",
		ReadWrite:   []string{"/tmp/bulle-501/bulle/tmp"},
		Env:         map[string]string{},
	}

	writeProfilePermissionSummary("agent", p, &stderr, false)

	assertContains(t, stderr.String(), "    rw: $TMP/bulle/tmp\n")
}

func TestWriteProfilePermissionSummaryUsesBunTmpDirForPathFormatting(t *testing.T) {
	var stderr bytes.Buffer
	p := policy.Policy{
		Backend:     policy.BackendLinuxLandlock,
		ProjectPath: "/workspace",
		ReadWrite:   []string{"/tmp/bun-cache/work"},
		Env:         map[string]string{"BUN_TMPDIR": "/tmp/bun-cache"},
	}

	writeProfilePermissionSummary("agent", p, &stderr, false)

	assertContains(t, stderr.String(), "    rw: $TMP/work\n")
}

func TestCollapsePrivateAliasesRequiresPrivatePathSegment(t *testing.T) {
	formatter := pathSummaryFormatter{}
	got := formatter.formatList([]string{"/var/foo", "/privatevar/foo"}, summaryRO)
	want := "/var/foo, /privatevar/foo"
	if strings.Join(got, ", ") != want {
		t.Fatalf("formatList() = %q, want %q", strings.Join(got, ", "), want)
	}
}

func TestGroupSiblingPathsDeduplicatesNames(t *testing.T) {
	got := strings.Join(groupSiblingPaths([]string{"/a/b", "/a/b", "/a/c"}), ", ")
	want := "/a/{b,c}"
	if got != want {
		t.Fatalf("groupSiblingPaths() = %q, want %q", got, want)
	}
}

func TestPreRunSessionPasteIncludesProfileSummaryWithoutKeychainWarning(t *testing.T) {
	opts := cli.Options{Flags: cli.Flags{Profile: "claude"}}
	p := policy.Policy{
		Backend:     policy.BackendMacOSSeatbelt,
		ProjectPath: "/tmp/project",
		Command:     []string{"claude"},
		Env:         map[string]string{"PATH": "/bin"},
		MachLookup:  []string{"com.apple.SecurityServer"},
	}

	got := preRunSessionPaste(opts, p)

	assertContains(t, got, "For context, bulle launched this session")
	assertContains(t, got, `bulle profile "claude" permissions:`)
	assertNotContains(t, got, "warning: allowing macOS Keychain access")
}

func TestCommandWithSessionPermissionsUsesProfileSpecificLaunchFlags(t *testing.T) {
	summary := "permissions summary"
	tests := []struct {
		profile string
		command []string
		want    []string
	}{
		{"claude", []string{"/opt/bin/claude"}, []string{"/opt/bin/claude", "--system-prompt", summary}},
		{"pi", []string{"/opt/bin/pi"}, []string{"/opt/bin/pi", "--append-system-prompt", summary}},
		{"opencode", []string{"/opt/bin/opencode"}, []string{"/opt/bin/opencode", "--prompt", summary}},
		{"codex", []string{"/opt/bin/codex"}, []string{"/opt/bin/codex", summary}},
		{"codex", []string{"/opt/bin/codex", "--model", "gpt-5"}, []string{"/opt/bin/codex", "--model", "gpt-5", summary}},
		{"pi", []string{"/usr/bin/env", "pi"}, []string{"/usr/bin/env", "pi"}},
	}
	for _, tt := range tests {
		got := commandWithSessionPermissions(tt.profile, tt.command, summary)
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
			t.Fatalf("commandWithSessionPermissions(%q, %#v) = %#v, want %#v", tt.profile, tt.command, got, tt.want)
		}
	}
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got string, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("output contains %q:\n%s", want, got)
	}
}

func TestShortenStorePath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/nix/store/y4nr67a046kgrvy79vhvn3jpa952xfg2-hosts", "/nix/store/y4nr67a…-hosts"},
		{"/nix/store/ga3b95jkyvknam1nxl25r95nyk87ix25-tzdata-2026b/share/zoneinfo", "/nix/store/ga3b95j…-tzdata-2026b/share/zoneinfo"},
		{"/usr/bin", "/usr/bin"},
		{"/nix/store/short-name", "/nix/store/short-name"},
		{"/nix/store/UPPERCASE046kgrvy79vhvn3jpa952xf-hosts", "/nix/store/UPPERCASE046kgrvy79vhvn3jpa952xf-hosts"},
	}
	for _, tt := range tests {
		if got := shortenStorePath(tt.in); got != tt.want {
			t.Errorf("shortenStorePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDropSubsumedRemovesCoveredEntries(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.WriteFile(child, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(child, link); err != nil {
		t.Fatal(err)
	}
	f := pathSummaryFormatter{granted: map[string]int{
		dir:         summaryRO,
		child:       summaryRO,
		link:        summaryRO,
		"/dev/null": summaryRO | summaryRW,
	}}
	got := f.dropSubsumed([]string{dir, child, link, "/dev/null"}, summaryRO)
	// child is covered by its granted parent; link stays because a granted
	// path carries its symlink target while a directory grant does not;
	// /dev/null is dropped from ro because rw holds it.
	want := []string{dir, link}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dropSubsumed() = %v, want %v", got, want)
	}
}

func TestCollapseSymlinkAliasesKeepsFirstSpelling(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	got, dropped := collapseSymlinkAliases([]string{real, alias, filepath.Join(dir, "other-missing")})
	if dropped != 1 || strings.Join(got, ",") != real+","+filepath.Join(dir, "other-missing") {
		t.Fatalf("collapseSymlinkAliases() = %v, dropped %d", got, dropped)
	}
}
