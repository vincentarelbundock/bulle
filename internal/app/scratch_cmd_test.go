package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListScratchesAndSelect(t *testing.T) {
	root := t.TempDir()
	origin1 := makeOrigin(t)
	origin2 := makeOrigin(t)
	var stderr bytes.Buffer
	s1, err := createScratch(origin1, root, nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := createScratch(origin2, root, nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	scratches, err := listScratches(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scratches) != 2 {
		t.Fatalf("listScratches found %d, want 2", len(scratches))
	}
	for _, s := range scratches {
		if s.BaselineTree == "" || s.Origin == "" {
			t.Fatalf("reconstructed state incomplete: %+v", s)
		}
	}

	// Exact id, unique prefix, ambiguous empty id with two scratches.
	got, err := selectScratch(scratches, s1.ID)
	if err != nil || got.Dir != s1.Dir {
		t.Fatalf("select by id: %v %v", got, err)
	}
	got, err = selectScratch(scratches, s2.ID[:4])
	if err != nil || got.Dir != s2.Dir {
		t.Fatalf("select by prefix: %v %v", got, err)
	}
	if _, err := selectScratch(scratches, ""); err == nil {
		t.Fatal("ambiguous selection must error")
	}
	if _, err := selectScratch(scratches, "zzzz"); err == nil {
		t.Fatal("unknown id must error")
	}
}

func TestRunScratchCommandListAndPull(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	// scratchCommandRoot resolves via config + stateRoot; XDG override makes
	// the root <tmp>/bulle/scratch on Linux.
	origin := makeOrigin(t)
	var stdout, stderr bytes.Buffer
	s, err := createScratch(origin, "", nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)
	if err := os.WriteFile(filepath.Join(s.Dir, "agent.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runScratchCommand([]string{"list"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("list exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), s.ID) || !strings.Contains(stdout.String(), origin) {
		t.Fatalf("list output missing scratch:\n%s", stdout.String())
	}

	// Dirty scratch: pull refuses and points at shell.
	stdout.Reset()
	stderr.Reset()
	if code := runScratchCommand([]string{"pull", s.ID}, &stdout, &stderr); code == ExitOK {
		t.Fatal("pull of a dirty scratch must fail")
	}
	if !strings.Contains(stderr.String(), "uncommitted") {
		t.Fatalf("expected uncommitted warning:\n%s", stderr.String())
	}

	// Committed: pull succeeds and removes the scratch.
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	stdout.Reset()
	stderr.Reset()
	if code := runScratchCommand([]string{"pull", s.ID}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("pull exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(origin, "agent.txt")); err != nil {
		t.Fatalf("origin missing pulled file: %v", err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("scratch not removed after pull")
	}

	stdout.Reset()
	if code := runScratchCommand([]string{"list"}, &stdout, &stderr); code != ExitOK || !strings.Contains(stdout.String(), "no scratches") {
		t.Fatalf("expected empty list, got %d: %s", code, stdout.String())
	}
}

func TestRunScratchCommandWipeNonInteractive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	origin := makeOrigin(t)
	var stdout, stderr bytes.Buffer
	s, err := createScratch(origin, "", nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code := runScratchCommand([]string{"wipe", s.ID}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("wipe exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("scratch not wiped")
	}
	if _, err := os.Stat(scratchMetaPath(s.Dir)); !os.IsNotExist(err) {
		t.Fatal("meta not wiped")
	}
}

func TestRunScratchCommandUnknownVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runScratchCommand([]string{"frobnicate"}, &stdout, &stderr); code != ExitConfigError {
		t.Fatalf("unknown verb exit %d", code)
	}
	if code := runScratchCommand(nil, &stdout, &stderr); code != ExitConfigError {
		t.Fatalf("missing verb exit %d", code)
	}
}

func TestScratchArgsStartRun(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		args    []string
		wantRun bool
		wantErr string
	}{
		{name: "no arguments stay with the verb usage", args: nil},
		{name: "review verb", args: []string{"diff", "a1b2"}},
		{name: "flag-led invocation runs", args: []string{"--profile", "claude"}, wantRun: true},
		{name: "separator runs", args: []string{"--", "shell"}, wantRun: true},
		{name: "existing directory is a workspace", args: []string{dir}, wantRun: true},
		{name: "command on PATH runs", args: []string{"go", "test"}, wantRun: true},
		{name: "near-miss verb is a typo", args: []string{"difff"}, wantErr: `did you mean "diff"`},
		{name: "dropped letter is a typo", args: []string{"wip"}, wantErr: `did you mean "wipe"`},
		{name: "unrelated unknown command runs", args: []string{"zzzsomecommand"}, wantRun: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scratchArgsStartRun(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantRun {
				t.Fatalf("startRun = %v, want %v", got, tc.wantRun)
			}
		})
	}
}

func TestEditDistanceWithin1(t *testing.T) {
	within := [][2]string{{"diff", "diff"}, {"difff", "diff"}, {"dif", "diff"}, {"daff", "diff"}, {"wip", "wipe"}, {"shel", "shell"}}
	beyond := [][2]string{{"policy", "pull"}, {"dfif", "diff"}, {"", "diff"}, {"listing", "list"}}
	for _, pair := range within {
		if !editDistanceWithin1(pair[0], pair[1]) {
			t.Errorf("editDistanceWithin1(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
	for _, pair := range beyond {
		if editDistanceWithin1(pair[0], pair[1]) {
			t.Errorf("editDistanceWithin1(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}
