package app

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// scratchState describes a disposable clone of the workspace created for a
// --scratch run. The sandbox workspace is retargeted to Dir; Origin is never
// granted, so nothing in the scratch can reach it during the run.
type scratchState struct {
	Dir          string
	Origin       string
	ID           string
	BaseCommit   string
	BaselineTree string
}

func scratchRoot(configured string) string {
	if configured != "" {
		return configured
	}
	root := stateRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "bulle", "scratch")
}

// createScratch clones origin into a disposable workspace: a local clone at
// origin's HEAD, with uncommitted tracked changes and untracked non-ignored
// files carried over so the agent starts from the state the user sees. The
// scratch is built under a temporary name and renamed into place only on full
// success, so a failure can never leave a torn copy that looks reviewable.
func createScratch(origin string, configuredRoot string, invocation []string, stderr io.Writer) (*scratchState, error) {
	origin, err := filepath.Abs(origin)
	if err != nil {
		return nil, err
	}
	if _, err := runGit(origin, "rev-parse", "--verify", "HEAD"); err != nil {
		return nil, fmt.Errorf("--scratch requires a git repository with at least one commit; run git init && git add -A && git commit first")
	}
	root := scratchRoot(configuredRoot)
	if root == "" {
		return nil, fmt.Errorf("--scratch: could not determine the state directory")
	}
	if strings.HasPrefix(origin+string(filepath.Separator), root+string(filepath.Separator)) {
		return nil, fmt.Errorf("--scratch: refusing to create a scratch of a workspace inside the scratch directory %s", root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if !sameFilesystem(origin, root) {
		fmt.Fprintf(stderr, "bulle: scratch directory %s is on a different filesystem than the workspace; git objects will be copied, not hardlinked\n", root)
	}
	if _, err := os.Stat(filepath.Join(origin, ".gitmodules")); err == nil {
		fmt.Fprintln(stderr, "bulle: warning: submodules are not carried into the scratch")
	}

	tmp, err := os.MkdirTemp(root, ".partial-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }

	base, err := runGit(origin, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return nil, err
	}
	branch, _ := runGit(origin, "symbolic-ref", "--short", "-q", "HEAD")

	if _, err := runGit("", "clone", "--local", "--no-checkout", "--quiet", origin, tmp); err != nil {
		cleanup()
		return nil, fmt.Errorf("--scratch: clone failed: %w", err)
	}
	if branch != "" {
		_, err = runGit(tmp, "checkout", "--quiet", "-B", branch, base)
	} else {
		_, err = runGit(tmp, "checkout", "--quiet", "--detach", base)
	}
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("--scratch: checkout failed: %w", err)
	}

	modified, untracked, err := carryDirtyState(origin, tmp, base)
	if err != nil {
		cleanup()
		return nil, err
	}

	baseline, err := worktreeTree(tmp)
	if err != nil {
		cleanup()
		return nil, err
	}

	id, err := randomID()
	if err != nil {
		cleanup()
		return nil, err
	}
	dir := filepath.Join(root, filepath.Base(origin)+"-"+id)
	if err := os.Rename(tmp, dir); err != nil {
		cleanup()
		return nil, err
	}
	s := &scratchState{Dir: dir, Origin: origin, ID: id, BaseCommit: base, BaselineTree: baseline}
	writeScratchMeta(s, invocation)

	carried := "clean"
	if modified > 0 || untracked > 0 {
		carried = fmt.Sprintf("%d modified, %d untracked", modified, untracked)
	}
	fmt.Fprintf(stderr, "bulle: scratch %s (origin %s, from HEAD + %s)\n", dir, origin, carried)
	return s, nil
}

// carryDirtyState applies the origin's uncommitted tracked changes and copies
// untracked non-ignored files into the scratch. A diff that fails to apply is
// a hard error: proceeding would run the agent from a state neither the user
// nor the agent expects.
func carryDirtyState(origin, scratch, base string) (modified int, untracked int, err error) {
	diff, err := runGitRaw(origin, "diff", "--binary", base)
	if err != nil {
		return 0, 0, err
	}
	if len(bytes.TrimSpace(diff)) > 0 {
		names, _ := runGit(origin, "diff", "--name-only", base)
		modified = len(strings.Fields(names))
		cmd := exec.Command("git", "-C", scratch, "apply", "--binary", "--whitespace=nowarn")
		cmd.Stdin = bytes.NewReader(diff)
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf
		if err := cmd.Run(); err != nil {
			return 0, 0, fmt.Errorf("--scratch: uncommitted changes do not apply cleanly to a fresh checkout: %s", strings.TrimSpace(errBuf.String()))
		}
	}
	listing, err := runGitRaw(origin, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return 0, 0, err
	}
	for _, name := range strings.Split(string(listing), "\x00") {
		if name == "" {
			continue
		}
		if err := copyFile(filepath.Join(origin, name), filepath.Join(scratch, name)); err != nil {
			return 0, 0, fmt.Errorf("--scratch: copying untracked %s: %w", name, err)
		}
		untracked++
	}
	return modified, untracked, nil
}

// worktreeTree hashes the scratch working tree (tracked and untracked alike)
// into a git tree object using a throwaway index, without touching the
// scratch's real index or creating commits. Comparing these hashes detects
// changes even when the agent committed its work, which leaves git status
// clean over very real unreviewed changes.
func worktreeTree(scratch string) (string, error) {
	index, err := os.CreateTemp(filepath.Dir(scratch), ".index-")
	if err != nil {
		return "", err
	}
	indexPath := index.Name()
	index.Close()
	os.Remove(indexPath) // git add refuses a zero-byte index file
	defer os.Remove(indexPath)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	add := exec.Command("git", "-C", scratch, "add", "-A")
	add.Env = env
	if out, err := add.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %s", strings.TrimSpace(string(out)))
	}
	write := exec.Command("git", "-C", scratch, "write-tree")
	write.Env = env
	out, err := write.Output()
	if err != nil {
		return "", fmt.Errorf("git write-tree: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// reviewScratch runs the post-command review gate. It never changes the exit
// code of the run, and never deletes a scratch that holds changes except on
// an explicit, confirmed discard.
func reviewScratch(s *scratchState, skipPrompt bool, stdout, stderr io.Writer) {
	final, err := worktreeTree(s.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: scratch kept at %s (could not compute changes: %v)\n", s.Dir, err)
		return
	}
	if final == s.BaselineTree {
		removeScratch(s)
		fmt.Fprintln(stderr, "bulle: scratch removed: the run changed nothing")
		return
	}
	lines, added, deleted, changed := scratchChangeSummary(s, final)
	fmt.Fprintf(stdout, "scratch: %d files changed, %d added, %d deleted\n", changed, added, deleted)
	for _, line := range lines {
		fmt.Fprintf(stdout, "  %s\n", line)
	}
	if skipPrompt || !stdinIsTerminal() {
		printScratchRecipe(s, stdout)
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(stdout, "[d]iff  [p]ull  [k]eep  [s]hell  [w]ipe? ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			printScratchRecipe(s, stdout)
			return
		}
		switch strings.TrimSpace(answer) {
		case "d":
			pageScratchDiff(s, final, stderr)
		case "p":
			// Pull moves commits only, and committing is the user's call: a
			// dirty scratch routes through [s] rather than an auto-commit.
			if scratchIsDirty(s) {
				fmt.Fprintln(stdout, "the scratch has uncommitted changes; use [s] to open a shell there and commit (or clean the tree), then [p] again")
				continue
			}
			if pullScratch(s, stdout, stderr) {
				return
			}
			// Failed pull: the scratch is untouched; re-prompt so the user
			// can inspect or keep it.
		case "k":
			printScratchRecipe(s, stdout)
			return
		case "s":
			openScratchShell(s, stdout, stderr)
			// The user may have committed, amended, or cleaned in the shell;
			// recompute so [p] and an emptied scratch behave correctly.
			newFinal, err := worktreeTree(s.Dir)
			if err == nil {
				final = newFinal
			}
			if final == s.BaselineTree && !scratchIsDirty(s) {
				removeScratch(s)
				fmt.Fprintln(stderr, "bulle: scratch removed: no changes remain")
				return
			}
		case "w":
			fmt.Fprintf(stdout, "wipe %d changed files? [y/N] ", changed+added)
			confirm, err := reader.ReadString('\n')
			if err == nil && strings.TrimSpace(confirm) == "y" {
				removeScratch(s)
				fmt.Fprintln(stdout, "scratch wiped")
				return
			}
		default:
			// re-prompt
		}
	}
}

func scratchIsDirty(s *scratchState) bool {
	status, err := runGit(s.Dir, "status", "--porcelain")
	return err != nil || status != ""
}

// pullScratch integrates the scratch's commits into the origin with git pull
// run from the origin side, so the merge updates the origin working tree
// properly. Only called on a clean scratch. On success the scratch is
// removed; on failure (conflicts, dirty origin) nothing is retried and the
// scratch is kept untouched.
func pullScratch(s *scratchState, stdout, stderr io.Writer) bool {
	cmd := exec.Command("git", "-C", s.Origin, "pull", "--no-rebase", s.Dir)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "bulle: pull failed; the scratch is untouched at %s\n", s.Dir)
		return false
	}
	removeScratch(s)
	fmt.Fprintf(stdout, "pulled into %s; scratch removed\n", s.Origin)
	return true
}

func scratchChangeSummary(s *scratchState, final string) (lines []string, added, deleted, changed int) {
	out, err := runGit(s.Dir, "diff", "--name-status", s.BaselineTree, final)
	if err != nil {
		return nil, 0, 0, 0
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, line)
		switch line[0] {
		case 'A':
			added++
		case 'D':
			deleted++
		default:
			changed++
		}
	}
	return lines, added, deleted, changed
}

// pageScratchDiff shows the full diff with the user's terminal inherited, so
// git's own pager takes over.
func pageScratchDiff(s *scratchState, final string, stderr io.Writer) {
	cmd := exec.Command("git", "-C", s.Dir, "diff", s.BaselineTree, final)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "bulle: diff failed: %v\n", err)
	}
}

// openScratchShell keeps the scratch and starts the user's shell inside it —
// unsandboxed, since the run is over and this is the user reviewing. A child
// process cannot change the parent shell's directory, so a subshell is the
// closest honest equivalent of "cd into the scratch".
func openScratchShell(s *scratchState, stdout, stderr io.Writer) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	fmt.Fprintf(stdout, "starting %s in %s (exit to return)\n", filepath.Base(shell), s.Dir)
	cmd := exec.Command(shell)
	cmd.Dir = s.Dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			fmt.Fprintf(stderr, "bulle: could not start %s: %v\n", shell, err)
		}
	}
}

// printScratchRecipe prints git-native integration steps, led by the pull
// form (one command from the origin side). Pull and push only move commits,
// so when the scratch working tree is dirty the recipe warns and leads with
// the commit step instead of silently integrating a partial result.
func printScratchRecipe(s *scratchState, w io.Writer) {
	fmt.Fprintf(w, "scratch kept: %s\n", s.Dir)
	status, err := runGit(s.Dir, "status", "--porcelain")
	dirty := err != nil || status != ""
	if dirty {
		fmt.Fprintln(w, "the scratch has uncommitted changes; integration only sees commits, so commit first:")
		fmt.Fprintf(w, "  git -C %s add -A && git -C %s commit\n", s.Dir, s.Dir)
		fmt.Fprintln(w, "then integrate from your repository:")
	} else {
		fmt.Fprintln(w, "all changes are committed; to integrate, from your repository:")
	}
	fmt.Fprintf(w, "  git -C %s pull %s\n", s.Origin, s.Dir)
	fmt.Fprintf(w, "  rm -rf %s\n", s.Dir)
	fmt.Fprintf(w, "to inspect before merging: git -C %s fetch %s HEAD:scratch/%s\n", s.Origin, s.Dir, s.ID)
}

func removeScratch(s *scratchState) {
	os.RemoveAll(s.Dir)
	os.Remove(scratchMetaPath(s.Dir))
}

func scratchMetaPath(dir string) string {
	return dir + ".meta.toml"
}

// writeScratchMeta records scratch metadata beside (not inside) the scratch,
// so the agent never sees it and it never appears in the change set. Nothing
// reads it yet; it exists so future tooling needs no format migration.
func writeScratchMeta(s *scratchState, invocation []string) {
	var b strings.Builder
	fmt.Fprintf(&b, "origin = %q\n", s.Origin)
	fmt.Fprintf(&b, "base_commit = %q\n", s.BaseCommit)
	fmt.Fprintf(&b, "baseline_tree = %q\n", s.BaselineTree)
	fmt.Fprintf(&b, "created = %q\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "invocation = %q\n", "bulle "+strings.Join(invocation, " "))
	_ = os.WriteFile(scratchMetaPath(s.Dir), []byte(b.String()), 0o600)
}

// rewriteScratchPaths maps scratch paths in denial hints back to
// origin-relative form, so a suggested grant is meaningful on the next run.
func rewriteScratchPaths(hints []string, s *scratchState, home string) []string {
	if s == nil {
		return hints
	}
	pairs := [][2]string{{s.Dir, s.Origin}}
	if home != "" {
		if rel, err := filepath.Rel(home, s.Dir); err == nil && !strings.HasPrefix(rel, "..") {
			pairs = append(pairs, [2]string{"~/" + rel, s.Origin})
		}
	}
	out := make([]string, len(hints))
	for i, hint := range hints {
		for _, pair := range pairs {
			hint = strings.ReplaceAll(hint, pair[0], pair[1])
		}
		out[i] = hint
	}
	return out
}

func runGit(dir string, args ...string) (string, error) {
	out, err := runGitRaw(dir, args...)
	return strings.TrimSpace(string(out)), err
}

func runGitRaw(dir string, args ...string) ([]byte, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sameFilesystem(a, b string) bool {
	statA, errA := os.Stat(a)
	statB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return true // unknown: stay quiet rather than warn spuriously
	}
	sysA, okA := statA.Sys().(*syscall.Stat_t)
	sysB, okB := statB.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return true
	}
	return sysA.Dev == sysB.Dev
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func randomID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
