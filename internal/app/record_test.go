package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRecordArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantBase  string
		wantOut   string
		wantRuns  []string
		wantMax   int
		wantError bool
	}{
		{
			name:     "explicit separator",
			args:     []string{"--profile", "claude", "--", "claude", "--version"},
			wantBase: "claude", wantRuns: []string{"claude", "--version"}, wantMax: defaultRecordRounds,
		},
		{
			name:     "run arguments without a separator",
			args:     []string{"--profile", "go", "go", "build", "./..."},
			wantBase: "go", wantRuns: []string{"go", "build", "./..."}, wantMax: defaultRecordRounds,
		},
		{
			name:     "out and max-rounds",
			args:     []string{"-p", "r", "-o", "out.toml", "--max-rounds", "3", "--", "Rscript", "-e", "1"},
			wantBase: "r", wantOut: "out.toml", wantMax: 3, wantRuns: []string{"Rscript", "-e", "1"},
		},
		{
			name:     "run flags pass through untouched",
			args:     []string{"--profile", "go", "--no-network", "--", "go", "test"},
			wantBase: "go", wantMax: defaultRecordRounds,
			wantRuns: []string{"--no-network", "--", "go", "test"},
		},
		{name: "missing profile", args: []string{"--", "ls"}, wantError: true},
		{name: "missing command", args: []string{"--profile", "go"}, wantError: true},
		{name: "profile without a value", args: []string{"--profile"}, wantError: true},
		{name: "zero rounds", args: []string{"-p", "go", "--max-rounds", "0", "--", "go"}, wantError: true},
		{name: "non-numeric rounds", args: []string{"-p", "go", "--max-rounds", "many", "--", "go"}, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseRecordArgs(tc.args)
			if tc.wantError {
				if err == nil {
					t.Fatalf("parseRecordArgs(%v) = %+v, want an error", tc.args, opts)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opts.base != tc.wantBase || opts.out != tc.wantOut || opts.maxRounds != tc.wantMax {
				t.Errorf("opts = %+v, want base=%q out=%q max=%d", opts, tc.wantBase, tc.wantOut, tc.wantMax)
			}
			if strings.Join(opts.runArgs, " ") != strings.Join(tc.wantRuns, " ") {
				t.Errorf("runArgs = %v, want %v", opts.runArgs, tc.wantRuns)
			}
		})
	}
}

func TestWithGrantFlagsKeepsTheCommandSeparatorLast(t *testing.T) {
	got := withGrantFlags(
		[]string{"bulle", "--profile", "go", "--", "go", "build"},
		[]string{"--ro", "/etc/a", "--rox", "/usr/bin/x"},
	)
	want := []string{"bulle", "--ro", "/etc/a", "--rox", "/usr/bin/x", "--profile", "go", "--", "go", "build"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestIsProbeArtifact(t *testing.T) {
	tmp := os.TempDir()
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}
	if !isProbeArtifact(filepath.Join(tmp, probeDirPrefix+"123", "denied")) {
		t.Error("probe temp file is not recognized")
	}
	if isProbeArtifact(filepath.Join(tmp, "something-else", "denied")) {
		t.Error("unrelated temp file recognized as a probe artifact")
	}
	// A directory of that name outside the temp root belongs to whoever made
	// it, and a denial there is the command's business.
	if isProbeArtifact(filepath.Join("/home/user", probeDirPrefix+"123", "denied")) {
		t.Error("path outside the temp root recognized as a probe artifact")
	}
}

func TestWriteRecordedListMarksEveryEntryOptional(t *testing.T) {
	var b strings.Builder
	writeRecordedList(&b, "ro", []recordedEntry{
		{List: "ro", Entry: "$CONFIG/b"},
		{List: "ro", Entry: "go:modcache", Comment: "for /home/user/go/pkg/mod/x/y.go"},
		{List: "rw", Entry: "$HOME/.cache/other"},
	})
	got := b.String()
	// A recorded path this machine has is not one the next machine has, so an
	// entry that fails to resolve must be skipped rather than fatal.
	for _, want := range []string{`"?$CONFIG/b",`, `"?go:modcache",`, "# for /home/user/go/pkg/mod/x/y.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "other") {
		t.Errorf("output includes an entry from a different list:\n%s", got)
	}
	if !strings.HasPrefix(got, "\nro = [\n") || !strings.HasSuffix(got, "]\n") {
		t.Errorf("output is not a TOML array:\n%s", got)
	}
}

func TestWriteRecordedListSkipsEmptyLists(t *testing.T) {
	var b strings.Builder
	writeRecordedList(&b, "rwx", []recordedEntry{{List: "ro", Entry: "$CONFIG/b"}})
	if b.String() != "" {
		t.Errorf("empty list emitted %q", b.String())
	}
}

func TestBuildRecordedProfileReportsWhatTheRunProves(t *testing.T) {
	opts := recordOptions{base: "go", runArgs: []string{"go", "build"}}

	capped := buildRecordedProfile(opts, newRecorder(), recordOutcome{rounds: 10, exit: 1, capped: true})
	if !strings.Contains(capped, "INCOMPLETE") {
		t.Errorf("capped recording does not say it is incomplete:\n%s", capped)
	}

	stalled := buildRecordedProfile(opts, newRecorder(), recordOutcome{rounds: 2, exit: 1})
	if !strings.Contains(stalled, "not sufficient") {
		t.Errorf("failed recording does not say the entries are insufficient:\n%s", stalled)
	}

	ok := buildRecordedProfile(opts, newRecorder(), recordOutcome{rounds: 1, exit: 0})
	if strings.Contains(ok, "INCOMPLETE") || strings.Contains(ok, "not sufficient") {
		t.Errorf("successful recording carries a failure warning:\n%s", ok)
	}
	for _, want := range []string{`inherits = ["go"]`, "# Command: go build", "denied nothing"} {
		if !strings.Contains(ok, want) {
			t.Errorf("output missing %q:\n%s", want, ok)
		}
	}
}

func TestAppendOrigins(t *testing.T) {
	if got := appendOrigins("", nil); got != "" {
		t.Errorf("no origins produced %q, want empty", got)
	}
	if got := appendOrigins("", []string{"mytool"}); got != "denied to mytool" {
		t.Errorf("got %q", got)
	}
	if got := appendOrigins("for /x/y", []string{"mytool", "helper"}); got != "for /x/y (denied to mytool, helper)" {
		t.Errorf("got %q", got)
	}
}

func TestRecorderDeduplicatesGrantsButAccumulatesOrigins(t *testing.T) {
	// The same path denied to several processes is one grant, but every
	// process that hit it is worth showing.
	rec := newRecorder()
	gr := grant{Flag: "--ro", Path: "/etc/a"}
	rec.noteOrigin(gr, "mytool")
	rec.noteOrigin(gr, "helper")
	rec.noteOrigin(gr, "mytool")
	rec.noteOrigin(gr, "")
	if got := rec.origins[gr]; len(got) != 2 || got[0] != "mytool" || got[1] != "helper" {
		t.Errorf("origins = %v, want [mytool helper]", got)
	}
}

func TestStallExplanationDistinguishesNoDenialsFromCoveredOnes(t *testing.T) {
	// No denials at all: the sandbox is not what failed, and the environment
	// is the usual culprit — something recording cannot discover.
	none := strings.Join(stallExplanation(0), " ")
	if !strings.Contains(none, "environment variable") || !strings.Contains(none, "--env") {
		t.Errorf("zero-denial stall does not point at the environment: %q", none)
	}
	// Denials seen but all covered: a different problem entirely.
	covered := strings.Join(stallExplanation(3), " ")
	if strings.Contains(covered, "environment variable") {
		t.Errorf("covered-denial stall blames the environment: %q", covered)
	}
	if !strings.Contains(covered, "already granted") {
		t.Errorf("covered-denial stall does not explain itself: %q", covered)
	}
}
