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
	"time"

	"golang.org/x/sys/unix"

	"github.com/vincentarelbundock/bulle/internal/record"
	"github.com/vincentarelbundock/bulle/internal/trustedexec"
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

	// --no-hardlinks is a security property, not a performance preference.
	// The scratch is writable by hostile code; sharing object-file inodes with
	// the origin would let that code corrupt the supposedly unreachable source
	// repository by chmodding and overwriting a hardlinked object.
	if _, err := runGit("", "clone", "--local", "--no-hardlinks", "--no-checkout", "--quiet", origin, tmp); err != nil {
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
	if err := saveScratchGitConfig(s); err != nil {
		os.RemoveAll(dir)
		os.Remove(scratchMetaPath(dir))
		return nil, fmt.Errorf("--scratch: %w", err)
	}

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
		cmd, err := trustedGitCommand("-C", scratch, "apply", "--binary", "--whitespace=nowarn")
		if err != nil {
			return 0, 0, err
		}
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
	add, err := trustedGitCommand(scratchGitArgs(scratch, "add", "-A")...)
	if err != nil {
		return "", err
	}
	add.Env = env
	if out, err := add.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %s", strings.TrimSpace(string(out)))
	}
	write, err := trustedGitCommand(scratchGitArgs(scratch, "write-tree")...)
	if err != nil {
		return "", err
	}
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
	// The run is over but its output is still on disk, and the next thing that
	// happens is bulle running git in a repository the run could write to.
	sanitizeScratchGit(s)
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
			pullScratch(s, stdout, stderr)
			return
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
	status, err := runScratchGit(s.Dir, "status", "--porcelain")
	return err != nil || status != ""
}

// pullScratch integrates the scratch's commits into the origin with git pull
// run from the origin side, so the merge updates the origin working tree
// properly. Only called on a clean scratch. On success the scratch is
// removed. On failure the scratch is kept and the review ends: the next
// steps live in the origin, not in bulle. A merge conflict in particular
// leaves the origin mid-merge, and the user resolves it there with normal
// git; the scratch stays around until they wipe it themselves.
func pullScratch(s *scratchState, stdout, stderr io.Writer) bool {
	cmd, err := trustedGitCommand("-C", s.Origin, "pull", "--no-rebase", s.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: pull failed: %v\n", err)
		return false
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if _, mergeErr := runGit(s.Origin, "rev-parse", "-q", "--verify", "MERGE_HEAD"); mergeErr == nil {
			fmt.Fprintf(stderr, "bulle: merge conflict: %s is now mid-merge\n", s.Origin)
			fmt.Fprintf(stderr, "resolve the conflicts there (git status shows them), then git add and git commit\n")
			fmt.Fprintf(stderr, "the scratch is kept at %s until you remove it: rm -rf %s\n", s.Dir, shellQuote(s.Dir))
		} else {
			fmt.Fprintf(stderr, "bulle: pull failed and nothing was merged; the origin and the scratch are untouched\n")
			printScratchRecipe(s, stderr)
		}
		return false
	}
	removeScratch(s)
	fmt.Fprintf(stdout, "pulled into %s; scratch removed\n", s.Origin)
	return true
}

func scratchChangeSummary(s *scratchState, final string) (lines []string, added, deleted, changed int) {
	out, err := runScratchGit(s.Dir, "diff", "--name-status", s.BaselineTree, final)
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
	cmd, err := trustedGitCommand(scratchGitArgs(s.Dir, "diff", s.BaselineTree, final)...)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: diff failed: %v\n", err)
		return
	}
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
	// The shell verb can be reached without going through the review gate, and
	// the shell that opens is the user's own: it must not inherit hooks or
	// filters the run left in the scratch's configuration.
	sanitizeScratchGit(s)
	configureScratchRemotes(s, stdout)
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

// scratchLocalRemote is the name the scratch's clone-time remote takes when a
// remote carried over from the source repository already claims "origin".
const scratchLocalRemote = "source"

// configureScratchRemotes copies the source repository's remotes into the
// scratch and prints the push-and-PR recipe. It runs when the review shell
// opens, never while the command is running: the shell is unsandboxed and is
// the user, so their existing credentials push, while the confined run keeps
// seeing a scratch whose only remote is the local clone path — no forge URL,
// no token, nothing to push with.
//
// The names come from the source repository's configuration, which is outside
// the sandbox and so is not the run's to choose. "origin" in the scratch is
// the local clone path; when the source carries its own "origin" the local one
// is renamed rather than shadowed, so `git push origin` in the review shell
// means the forge, which is what someone typing it there intends.
func configureScratchRemotes(s *scratchState, stdout io.Writer) {
	remotes := sourceRemotes(s.Origin)
	if len(remotes) == 0 {
		return
	}
	existing := map[string]bool{}
	for _, name := range strings.Fields(strings.ReplaceAll(mustScratchGit(s.Dir, "remote"), "\n", " ")) {
		existing[name] = true
	}
	for _, remote := range remotes {
		if remote.name == "origin" && existing["origin"] && !existing[scratchLocalRemote] {
			if _, err := runScratchGit(s.Dir, "remote", "rename", "origin", scratchLocalRemote); err != nil {
				continue
			}
			existing[scratchLocalRemote] = true
			delete(existing, "origin")
		}
		if existing[remote.name] {
			continue
		}
		if _, err := runScratchGit(s.Dir, "remote", "add", remote.name, remote.url); err != nil {
			continue
		}
		existing[remote.name] = true
		if remote.pushURL != "" && remote.pushURL != remote.url {
			_, _ = runScratchGit(s.Dir, "remote", "set-url", "--push", remote.name, remote.pushURL)
		}
		fmt.Fprintf(stdout, "remote configured: %s -> %s\n", remote.name, remote.url)
	}
	printScratchPushRecipe(s, remotes, stdout)
}

// printScratchPushRecipe prints the branch-safe push. The refspec spelling
// matters: the scratch checks out the source's branch, so a bare `git push`
// would target that same branch on the forge. HEAD:scratch/<id> puts the work
// on a branch of its own, which is what a pull request wants and what keeps a
// scratch from ever landing on main by accident.
func printScratchPushRecipe(s *scratchState, remotes []sourceRemote, stdout io.Writer) {
	remote := remotes[0].name
	for _, candidate := range remotes {
		if candidate.name == "origin" {
			remote = candidate.name
			break
		}
	}
	branch := "scratch/" + s.ID
	fmt.Fprintln(stdout, "to open a pull request from this scratch:")
	fmt.Fprintf(stdout, "  git push -u %s HEAD:%s\n", remote, branch)
	if _, err := trustedexec.LookPath("gh", os.Getenv("PATH")); err == nil {
		fmt.Fprintf(stdout, "  gh pr create --head %s\n", branch)
	}
}

// sourceRemote is one remote of the source repository, as configured there.
type sourceRemote struct {
	name    string
	url     string
	pushURL string
}

// sourceRemotes reads the source repository's remotes, skipping any that
// point back into the scratch tree — a scratch of a scratch would otherwise
// suggest pushing a directory that is about to be deleted.
func sourceRemotes(origin string) []sourceRemote {
	names, err := runGit(origin, "remote")
	if err != nil {
		return nil
	}
	var out []sourceRemote
	for _, name := range strings.Fields(names) {
		url, err := runGit(origin, "remote", "get-url", name)
		if err != nil || url == "" {
			continue
		}
		if filepath.IsAbs(url) {
			continue
		}
		remote := sourceRemote{name: name, url: url}
		if push, err := runGit(origin, "remote", "get-url", "--push", name); err == nil {
			remote.pushURL = push
		}
		out = append(out, remote)
	}
	return out
}

// mustScratchGit returns the command's output, or the empty string when it
// fails: every caller treats a failure as "nothing configured yet".
func mustScratchGit(dir string, args ...string) string {
	out, err := runScratchGit(dir, args...)
	if err != nil {
		return ""
	}
	return out
}

// printScratchRecipe prints git-native integration steps, led by the pull
// form (one command from the origin side). Pull and push only move commits,
// so when the scratch working tree is dirty the recipe warns and leads with
// the commit step instead of silently integrating a partial result.
func printScratchRecipe(s *scratchState, w io.Writer) {
	// Every path in a copy-pasteable line is shell-quoted: the scratch name
	// ends in the workspace's basename, which may contain spaces, and an
	// unquoted "rm -rf" over a split path deletes something else entirely.
	dir, origin := shellQuote(s.Dir), shellQuote(s.Origin)
	fmt.Fprintf(w, "scratch kept: %s\n", s.Dir)
	status, err := runScratchGit(s.Dir, "status", "--porcelain")
	dirty := err != nil || status != ""
	if dirty {
		fmt.Fprintln(w, "the scratch has uncommitted changes; integration only sees commits, so commit first:")
		fmt.Fprintf(w, "  git -C %s add -A && git -C %s commit\n", dir, dir)
		fmt.Fprintln(w, "then integrate from your repository:")
	} else {
		fmt.Fprintln(w, "all changes are committed; to integrate, from your repository:")
	}
	fmt.Fprintf(w, "  git -C %s pull %s\n", origin, dir)
	fmt.Fprintf(w, "  rm -rf %s\n", dir)
	fmt.Fprintf(w, "to inspect before merging: git -C %s fetch %s HEAD:scratch/%s\n", origin, dir, s.ID)
	if len(sourceRemotes(s.Origin)) > 0 {
		fmt.Fprintf(w, "to push it to a branch instead: bulle scratch shell %s\n", s.ID)
	}
}

func removeScratch(s *scratchState) {
	os.RemoveAll(s.Dir)
	os.Remove(scratchMetaPath(s.Dir))
	os.Remove(scratchGitConfigPath(s.Dir))
}

func scratchMetaPath(dir string) string {
	return dir + ".meta.toml"
}

// scratchGitConfigPath is where the pristine copy of the scratch's git
// configuration is kept: beside the scratch, never inside it, so the sandboxed
// command has no way to reach the copy it is going to be compared against.
func scratchGitConfigPath(dir string) string {
	return dir + ".gitconfig"
}

// saveScratchGitConfig snapshots the configuration git wrote when it made the
// clone, before anything has run inside it.
func saveScratchGitConfig(s *scratchState) error {
	data, err := os.ReadFile(filepath.Join(s.Dir, ".git", "config"))
	if err != nil {
		return fmt.Errorf("reading the scratch's git configuration: %w", err)
	}
	return os.WriteFile(scratchGitConfigPath(s.Dir), data, 0o600)
}

// sanitizeScratchGit undoes anything the sandboxed command did to the scratch's
// git configuration before bulle runs git there again.
//
// The scratch's own .git/ sits inside the workspace, which is granted
// read-write — it has to be, or the agent could not commit. That makes every
// exec hook git reads from configuration (filter.<name>.clean, core.pager,
// core.hooksPath, diff.external, uploadpack.packObjectsHook, core.sshCommand)
// something the confined process can set and bulle would then run, unsandboxed
// and as the user, the moment the review gate touches the repository. Restoring
// the configuration git itself wrote at clone time and clearing the hook
// directory removes the whole class: a .gitattributes naming a filter driver
// that no configuration defines is inert.
func sanitizeScratchGit(s *scratchState) {
	if data, err := os.ReadFile(scratchGitConfigPath(s.Dir)); err == nil {
		_ = os.WriteFile(filepath.Join(s.Dir, ".git", "config"), data, 0o600)
	}
	hooks := filepath.Join(s.Dir, ".git", "hooks")
	if err := os.RemoveAll(hooks); err == nil {
		_ = os.Mkdir(hooks, 0o700)
	}
	// .git/info/attributes and .git/info/exclude are read as configuration too;
	// the clone creates neither, so anything there arrived from the run.
	_ = os.Remove(filepath.Join(s.Dir, ".git", "info", "attributes"))
}

// scratchGitArgs builds the argument list for a git command run against a
// scratch: the hardening overrides, then -C, then the command itself.
func scratchGitArgs(dir string, args ...string) []string {
	out := append(hardenedGitArgs(), "-C", dir)
	return append(out, args...)
}

// runScratchGit runs a git command against a scratch with the hardening
// overrides applied.
func runScratchGit(dir string, args ...string) (string, error) {
	return runGit("", scratchGitArgs(dir, args...)...)
}

// hardenedGitArgs are overrides bulle passes to every git command it runs
// against a scratch. They are belt to sanitizeScratchGit's braces: -c beats
// repository configuration, so even a configuration file restored from a stale
// or missing snapshot cannot route git through a program of the run's choosing.
func hardenedGitArgs() []string {
	return []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=",
		"-c", "diff.external=",
		"-c", "core.sshCommand=",
		"-c", "core.askPass=",
		"-c", "uploadpack.packObjectsHook=",
		"-c", "gpg.program=" + os.DevNull,
		"-c", "protocol.ext.allow=never",
	}
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
	cmd, err := trustedGitCommand(args...)
	if err != nil {
		return nil, err
	}
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

func trustedGitCommand(args ...string) (*exec.Cmd, error) {
	git, err := trustedexec.LookPath("git", os.Getenv("PATH"))
	if err != nil {
		return nil, fmt.Errorf("refusing to run git outside the sandbox: %w", err)
	}
	return exec.Command(git, args...), nil
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

// stdinIsTerminal is a real isatty check: /dev/null is a character device
// too, and a mode-bits test would leave cron jobs hanging on the prompt.
func stdinIsTerminal() bool {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), termiosRequest)
	return err == nil
}

// shellQuote renders a path as a single POSIX shell word, so a printed command
// stays one command however the path is spelled.
func shellQuote(path string) string {
	if path != "" && !strings.ContainsAny(path, " \t\n\"'\\$`&;|<>()*?[]#~!{}") {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

func randomID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// scratchRewrite exposes the scratch-to-origin mapping to the recorder, so a
// grant learned during a scratch run is saved against the real workspace.
func scratchRewrite(s *scratchState) *record.ScratchRewrite {
	if s == nil {
		return nil
	}
	return &record.ScratchRewrite{Dir: s.Dir, Origin: s.Origin}
}
