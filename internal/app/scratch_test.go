package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func makeOrigin(t *testing.T) string {
	t.Helper()
	origin := t.TempDir()
	gitT(t, origin, "init", "-q")
	// A repository-local identity, because bulle's own git calls run with the
	// inherited environment: git refuses to start a merge it could not commit,
	// and a machine with no global user.email (a CI runner) would otherwise
	// never reach the conflict the review gate is meant to hand off.
	gitT(t, origin, "config", "user.name", "t")
	gitT(t, origin, "config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(origin, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, origin, "add", "-A")
	gitT(t, origin, "commit", "-q", "-m", "initial")
	return origin
}

func TestCreateScratchCarriesDirtyState(t *testing.T) {
	origin := makeOrigin(t)
	if err := os.WriteFile(filepath.Join(origin, "tracked.txt"), []byte("v2 dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), []string{"--scratch"}, &stderr)
	if err != nil {
		t.Fatalf("createScratch: %v", err)
	}
	defer removeScratch(s)

	for name, want := range map[string]string{"tracked.txt": "v2 dirty\n", "untracked.txt": "new\n"} {
		data, err := os.ReadFile(filepath.Join(s.Dir, name))
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, %v; want %q", name, data, err, want)
		}
	}
	if !strings.Contains(stderr.String(), "1 modified, 1 untracked") {
		t.Errorf("carry report missing: %s", stderr.String())
	}
	// The scratch .git is self-contained (a directory, not a worktree pointer).
	info, err := os.Stat(filepath.Join(s.Dir, ".git"))
	if err != nil || !info.IsDir() {
		t.Errorf(".git not a self-contained directory: %v", err)
	}
	// The origin remote is kept for push-based integration.
	if remote := gitT(t, s.Dir, "remote", "get-url", "origin"); remote != origin {
		t.Errorf("origin remote = %q, want %q", remote, origin)
	}
	// Metadata lives beside the scratch, not inside it.
	if _, err := os.Stat(scratchMetaPath(s.Dir)); err != nil {
		t.Errorf("meta.toml missing: %v", err)
	}
}

func TestCreateScratchRequiresGit(t *testing.T) {
	var stderr bytes.Buffer
	_, err := createScratch(t.TempDir(), t.TempDir(), nil, &stderr)
	if err == nil || !strings.Contains(err.Error(), "git init") {
		t.Fatalf("want git-init suggestion, got %v", err)
	}
}

func TestCreateScratchRefusesNesting(t *testing.T) {
	root := t.TempDir()
	origin := makeOrigin(t)
	var stderr bytes.Buffer
	s, err := createScratch(origin, root, nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)
	_, err = createScratch(s.Dir, root, nil, &stderr)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("want nesting refusal, got %v", err)
	}
}

func TestScratchChangeDetection(t *testing.T) {
	origin := makeOrigin(t)
	var stderr bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)

	// Unchanged: final tree equals the baseline.
	final, err := worktreeTree(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if final != s.BaselineTree {
		t.Fatalf("expected empty change set, got %s vs %s", final, s.BaselineTree)
	}

	// Committed work must still count as a change even though git status is
	// clean: detection compares trees, not status.
	if err := os.WriteFile(filepath.Join(s.Dir, "agent.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	if status := gitT(t, s.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("expected clean status, got %q", status)
	}
	final, err = worktreeTree(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if final == s.BaselineTree {
		t.Fatal("committed work not detected as a change")
	}
	lines, added, deleted, changed := scratchChangeSummary(s, final)
	if added != 1 || deleted != 0 || changed != 0 || len(lines) != 1 {
		t.Fatalf("summary = %v added=%d deleted=%d changed=%d", lines, added, deleted, changed)
	}
}

func TestReviewScratchRemovesEmptyAndKeepsChanged(t *testing.T) {
	origin := makeOrigin(t)
	var stderr, stdout bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	reviewScratch(s, true, &stdout, &stderr)
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatalf("empty scratch not removed: %v", err)
	}

	s, err = createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)
	if err := os.WriteFile(filepath.Join(s.Dir, "agent.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	reviewScratch(s, true, &stdout, &stderr)
	if _, err := os.Stat(s.Dir); err != nil {
		t.Fatalf("changed scratch was removed: %v", err)
	}
	out := stdout.String()
	// The scratch working tree is dirty here, so the recipe must warn and
	// lead with the commit step before the pull form.
	for _, want := range []string{"1 added", "uncommitted changes", "pull " + s.Dir, "rm -rf " + s.Dir} {
		if !strings.Contains(out, want) {
			t.Errorf("review output missing %q:\n%s", want, out)
		}
	}

	// Once everything is committed, the recipe goes straight to the pull form.
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	var recipe bytes.Buffer
	printScratchRecipe(s, &recipe)
	if strings.Contains(recipe.String(), "uncommitted") {
		t.Errorf("clean scratch should not warn:\n%s", recipe.String())
	}
	if !strings.Contains(recipe.String(), "all changes are committed") {
		t.Errorf("clean recipe missing pull lead:\n%s", recipe.String())
	}
}

func TestScratchPushIntegration(t *testing.T) {
	origin := makeOrigin(t)
	var stderr bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)
	if err := os.WriteFile(filepath.Join(s.Dir, "agent.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	gitT(t, s.Dir, "push", "-q", "origin", "HEAD:scratch/"+s.ID)
	// The result lands as a ref in the origin, never as working-tree changes.
	if _, err := os.Stat(filepath.Join(origin, "agent.txt")); !os.IsNotExist(err) {
		t.Fatal("push must not touch the origin working tree")
	}
	gitT(t, origin, "merge", "-q", "scratch/"+s.ID)
	if _, err := os.Stat(filepath.Join(origin, "agent.txt")); err != nil {
		t.Fatalf("merge did not bring the work over: %v", err)
	}
}

func TestPullScratchIntegratesAndRemoves(t *testing.T) {
	origin := makeOrigin(t)
	var stderr, stdout bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "agent.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !scratchIsDirty(s) {
		t.Fatal("expected dirty scratch before commit")
	}
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	if scratchIsDirty(s) {
		t.Fatal("expected clean scratch after commit")
	}
	if !pullScratch(s, &stdout, &stderr) {
		t.Fatalf("pullScratch failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(origin, "agent.txt")); err != nil {
		t.Fatalf("origin missing pulled file: %v", err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("scratch not removed after successful pull")
	}
}

func TestPullScratchKeepsScratchOnFailure(t *testing.T) {
	origin := makeOrigin(t)
	var stderr, stdout bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)
	if err := os.WriteFile(filepath.Join(s.Dir, "tracked.txt"), []byte("scratch version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	// A conflicting uncommitted edit in the origin makes the merge refuse.
	if err := os.WriteFile(filepath.Join(origin, "tracked.txt"), []byte("origin version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pullScratch(s, &stdout, &stderr) {
		t.Fatal("pullScratch should fail when the origin would be overwritten")
	}
	if _, err := os.Stat(s.Dir); err != nil {
		t.Fatalf("scratch must survive a failed pull: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(origin, "tracked.txt")); string(data) != "origin version\n" {
		t.Fatalf("origin working tree modified by failed pull: %q", data)
	}
}

func TestPullScratchConflictHandsOffToOrigin(t *testing.T) {
	origin := makeOrigin(t)
	var stderr, stdout bytes.Buffer
	s, err := createScratch(origin, t.TempDir(), nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer removeScratch(s)
	// Divergent committed edits to the same file on both sides force a real
	// merge conflict, not an up-front refusal.
	if err := os.WriteFile(filepath.Join(s.Dir, "tracked.txt"), []byte("scratch version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, s.Dir, "add", "-A")
	gitT(t, s.Dir, "commit", "-q", "-m", "agent work")
	if err := os.WriteFile(filepath.Join(origin, "tracked.txt"), []byte("origin version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, origin, "add", "-A")
	gitT(t, origin, "commit", "-q", "-m", "origin work")

	stderr.Reset()
	if pullScratch(s, &stdout, &stderr) {
		t.Fatal("conflicting pull must not report success")
	}
	if _, err := runGit(origin, "rev-parse", "-q", "--verify", "MERGE_HEAD"); err != nil {
		t.Fatalf("origin should be mid-merge: %v", err)
	}
	for _, want := range []string{"mid-merge", "resolve the conflicts there", "rm -rf " + s.Dir} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("conflict guidance missing %q:\n%s", want, stderr.String())
		}
	}
	if _, err := os.Stat(s.Dir); err != nil {
		t.Fatalf("scratch must survive a conflicted pull: %v", err)
	}
}

func TestRewriteScratchPaths(t *testing.T) {
	s := &scratchState{Dir: "/home/u/.local/state/bulle/scratch/proj-ab12cd34", Origin: "/home/u/proj"}
	hints := []string{
		"denied: write /home/u/.local/state/bulle/scratch/proj-ab12cd34/x — add --rw /home/u/.local/state/bulle/scratch/proj-ab12cd34/x",
		"denied: read ~/.local/state/bulle/scratch/proj-ab12cd34/y — add --ro ~/.local/state/bulle/scratch/proj-ab12cd34/y",
	}
	out := rewriteScratchPaths(hints, s, "/home/u")
	if out[0] != "denied: write /home/u/proj/x — add --rw /home/u/proj/x" {
		t.Errorf("abs rewrite: %s", out[0])
	}
	if out[1] != "denied: read /home/u/proj/y — add --ro /home/u/proj/y" {
		t.Errorf("home-abbreviated rewrite: %s", out[1])
	}
}
