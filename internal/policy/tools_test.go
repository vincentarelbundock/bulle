package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

func writeToolTestExecutable(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func toolTestFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestParseResolverOutputKeepsAbsolutePathsSorted(t *testing.T) {
	got := parseResolverOutput("/b/two\n\n/a/one\nrelative/skipped\n/b/two\n", formatLines)
	want := []string{"/a/one", "/b/two"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parseResolverOutput = %v, want %v", got, want)
	}
}

func TestParseResolverOutputSingleIgnoresUnsetValue(t *testing.T) {
	if got := parseResolverOutput("  \n", formatSingle); len(got) != 0 {
		t.Fatalf("parseResolverOutput = %v, want empty", got)
	}
	if got := parseResolverOutput("  /tmp/x  \n", formatSingle); len(got) != 1 || got[0] != "/tmp/x" {
		t.Fatalf("parseResolverOutput = %v, want [/tmp/x]", got)
	}
}

// A tool that prints a relative path or a diagnostic line must not turn into a
// grant: the entry resolves to nothing and is reported, rather than silently
// granting something relative to the working directory.
func TestParseResolverOutputRejectsRelativePaths(t *testing.T) {
	if got := parseResolverOutput("not-a-path", formatSingle); len(got) != 0 {
		t.Fatalf("parseResolverOutput = %v, want empty", got)
	}
}

func TestExpandToolResolversUnknownToolIsAnError(t *testing.T) {
	_, _, err := expandToolResolvers([]string{"ruby:gems"}, "ro", "", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown resolver namespace")
	}
	if !strings.Contains(err.Error(), "unknown resolver") {
		t.Fatalf("error = %v, want it to name the unknown resolver", err)
	}
}

func TestExpandToolResolversUnknownAspectListsKnownOnes(t *testing.T) {
	_, _, err := expandToolResolvers([]string{"r:packages"}, "ro", "", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown aspect")
	}
	if !strings.Contains(err.Error(), "known aspects are") || !strings.Contains(err.Error(), "libs") {
		t.Fatalf("error = %v, want it to list the known aspects", err)
	}
}

// Literal paths must pass through untouched, including paths that contain a
// colon but are not written in resolver form.
func TestExpandToolResolversPassesLiteralPathsThrough(t *testing.T) {
	entries := []string{"/tmp/plain", "./ruby:gems", "$HOME/x", "~/y", "?/tmp/opt"}
	out, traces, err := expandToolResolvers(entries, "ro", "", nil)
	if err != nil {
		t.Fatalf("expandToolResolvers: %v", err)
	}
	if strings.Join(out, ",") != strings.Join(entries, ",") {
		t.Fatalf("entries = %v, want them unchanged", out)
	}
	if len(traces) != 0 {
		t.Fatalf("traces = %v, want none for literal paths", traces)
	}
}

// which:/pkg: are handled by the exec resolver pass, so the tool pass must
// leave them alone rather than reporting them as unknown namespaces.
func TestExpandToolResolversIgnoresExecResolvers(t *testing.T) {
	out, _, err := expandToolResolvers([]string{"which:ls", "pkg:node"}, "rox", "", nil)
	if err != nil {
		t.Fatalf("expandToolResolvers: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("entries = %v, want both passed through", out)
	}
}

func TestExpandToolResolversMissingToolIsFatalUnlessOptional(t *testing.T) {
	// An empty PATH guarantees the tool cannot be found.
	if _, _, err := expandToolResolvers([]string{"r:libs"}, "ro", "", nil); err == nil {
		t.Fatal("expected a missing tool to be an error")
	} else if !strings.Contains(err.Error(), "?") {
		t.Fatalf("error = %v, want it to mention the optional marker", err)
	}
	out, traces, err := expandToolResolvers([]string{"?r:libs"}, "ro", "", nil)
	if err != nil {
		t.Fatalf("optional entry returned an error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("entries = %v, want none granted", out)
	}
	if len(traces) != 1 || !strings.HasPrefix(traces[0].Outcome, "skipped") {
		t.Fatalf("traces = %v, want one skipped trace", traces)
	}
}

// The registry decides what runs. A profile naming a command directly must not
// be executed, because profiles are installable from GitHub.
func TestExpandToolResolversDoesNotRunProfileSuppliedCommands(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "evil")
	writeToolTestExecutable(t, script, "#!/bin/sh\ntouch "+marker+"\n")
	for _, entry := range []string{"evil:x", "sh:-c", dir + "/evil"} {
		if _, _, err := expandToolResolvers([]string{entry}, "ro", dir, nil); err == nil && strings.Contains(entry, ":") {
			t.Fatalf("entry %q was accepted; resolver namespaces must be registry-defined", entry)
		}
	}
	if toolTestFileExists(marker) {
		t.Fatal("a profile-supplied command was executed")
	}
}

func TestToolResolverRefusesWritablePATHExecutable(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "resolver-ran")
	fakeGo := filepath.Join(dir, "go")
	writeToolTestExecutable(t, fakeGo, "#!/bin/sh\ntouch \"$MARKER\"\nprintf '/tmp/attacker-controlled\\n'\n")
	_, _, err := expandToolResolvers(
		[]string{"go:path"},
		"rw",
		dir,
		map[string]string{"PATH": dir, "HOME": t.TempDir(), "MARKER": marker},
	)
	if err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("resolver error = %v, want writable-executable refusal", err)
	}
	if toolTestFileExists(marker) {
		t.Fatal("writable PATH resolver executed outside the sandbox")
	}
}

func TestResolverEnvironmentDropsSecrets(t *testing.T) {
	env := resolverEnvironment("go", "/usr/bin:/bin", map[string]string{
		"HOME": "/home/user", "PATH": "/usr/bin:/bin", "GOPATH": "/go",
		"OPENAI_API_KEY": "secret", "CODEX_CONNECTORS_TOKEN": "secret",
	})
	if env["GOPATH"] != "/go" {
		t.Fatalf("GOPATH = %q, want /go", env["GOPATH"])
	}
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_CONNECTORS_TOKEN"} {
		if _, ok := env[key]; ok {
			t.Fatalf("resolver environment retained secret %s", key)
		}
	}
}

func TestKnownResolverToolsAreSortedAndUnique(t *testing.T) {
	tools := KnownResolverTools()
	for i := 1; i < len(tools); i++ {
		if tools[i-1] >= tools[i] {
			t.Fatalf("KnownResolverTools = %v, want sorted and unique", tools)
		}
	}
}

// Every registry row must be reachable through the namespace parser, or the
// entry can never be written in a profile.
func TestEveryRegistryEntryParsesAsAResolver(t *testing.T) {
	for _, r := range toolResolvers {
		entry := r.tool + ":" + r.aspect
		namespace, aspect, ok := bpaths.ResolverNamespace(entry)
		if !ok || namespace != r.tool || aspect != r.aspect {
			t.Fatalf("entry %q parsed as (%q, %q, %v)", entry, namespace, aspect, ok)
		}
		if isExecResolverNamespace(namespace) {
			t.Fatalf("registry entry %q collides with an executable resolver namespace", entry)
		}
		if len(r.argv) == 0 {
			t.Fatalf("registry entry %q has no command", entry)
		}
	}
}

func TestRResolversDisableEveryUserStartupSource(t *testing.T) {
	for _, r := range toolResolvers {
		if r.tool != "r" {
			continue
		}
		joined := " " + strings.Join(r.argv, " ") + " "
		for _, flag := range []string{" --no-environ ", " --no-init-file ", " --no-site-file "} {
			if !strings.Contains(joined, flag) {
				t.Errorf("resolver r:%s argv %v lacks %s", r.aspect, r.argv, strings.TrimSpace(flag))
			}
		}
	}
}

// Markers describe what should happen to the paths an entry names, so they
// must survive expansion: an optional resolver whose directory does not exist
// yet must not become a hard failure in a rw list.
func TestExpandToolResolversPreservesMarkers(t *testing.T) {
	cases := []struct {
		entry  string
		prefix string
		suffix string
	}{
		{"go:path", "", ""},
		{"?go:path", "?", ""},
		{"+go:path", "+", "/"},
		{"?+go:path", "?+", "/"},
	}
	path := os.Getenv("PATH")
	for _, tc := range cases {
		out, _, err := expandToolResolvers([]string{tc.entry}, "rw", path, map[string]string{"PATH": path, "HOME": os.Getenv("HOME")})
		if err != nil {
			t.Skipf("go toolchain unavailable: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("entry %q expanded to %v, want one path", tc.entry, out)
		}
		if !strings.HasPrefix(out[0], tc.prefix) || !strings.HasSuffix(out[0], tc.suffix) {
			t.Errorf("entry %q expanded to %q, want prefix %q and suffix %q", tc.entry, out[0], tc.prefix, tc.suffix)
		}
	}
}

// The Xcode toolchain reads its bundle's Info.plist and frameworks, which sit
// above the directory xcode-select reports, so xcode:app walks up to the
// bundle. A Command Line Tools install has no enclosing bundle and must
// resolve to nothing rather than to the /Library/Developer tree above it.
func TestKeepAppBundleRoots(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "xcode developer directory maps to the bundle",
			in:   []string{"/Applications/Xcode_26.6.app/Contents/Developer"},
			want: []string{"/Applications/Xcode_26.6.app"},
		},
		{
			name: "command line tools have no bundle",
			in:   []string{"/Library/Developer/CommandLineTools"},
			want: []string{},
		},
		{
			name: "aspects of one bundle collapse to a single root",
			in: []string{
				"/Applications/Xcode.app/Contents/Developer",
				"/Applications/Xcode.app/Contents/SharedFrameworks",
			},
			want: []string{"/Applications/Xcode.app"},
		},
		{
			name: "the bundle itself is already a root",
			in:   []string{"/Applications/Xcode.app"},
			want: []string{"/Applications/Xcode.app"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := keepAppBundleRoots(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("keepAppBundleRoots(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("keepAppBundleRoots(%v) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}
