package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/exitcode"
)

// scratchMeta mirrors what writeScratchMeta records beside each scratch.
type scratchMeta struct {
	Origin       string `toml:"origin"`
	BaseCommit   string `toml:"base_commit"`
	BaselineTree string `toml:"baseline_tree"`
	Created      string `toml:"created"`
	Invocation   string `toml:"invocation"`
}

// scratchVerbs are the review verbs of `bulle scratch`; anything else after
// the word `scratch` starts a run in a fresh scratch instead.
var scratchVerbs = []string{"list", "diff", "pull", "wipe", "shell"}

func isScratchVerb(arg string) bool {
	for _, verb := range scratchVerbs {
		if arg == verb {
			return true
		}
	}
	return false
}

// scratchArgsStartRun reports whether `bulle scratch <args>` is a run in a
// fresh scratch rather than a review verb. Verbs never begin with a dash, so a
// flag-led invocation (bulle scratch --profile claude) is unambiguous; an
// unrecognized positional reads exactly as it would at the top level, where
// an existing directory is the workspace and anything else starts the command.
//
// The one thing that decision must not swallow is a mistyped verb: without a
// guard, `bulle scratch difff` would clone the repository just to fail on a
// missing executable. A near-miss that is neither a directory nor a command on
// PATH is reported as a typo instead.
func scratchArgsStartRun(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	first := args[0]
	if isScratchVerb(first) {
		return false, nil
	}
	if strings.HasPrefix(first, "-") {
		return true, nil
	}
	if info, err := os.Stat(first); err == nil && info.IsDir() {
		return true, nil
	}
	if _, err := exec.LookPath(first); err == nil {
		return true, nil
	}
	for _, verb := range scratchVerbs {
		if editDistanceWithin1(first, verb) {
			return false, fmt.Errorf("unknown scratch command %q; did you mean %q? (to run %q in a scratch, use: bulle scratch -- %s)", first, verb, first, first)
		}
	}
	return true, nil
}

// editDistanceWithin1 reports whether a and b are at most one single-character
// insertion, deletion, or substitution apart.
func editDistanceWithin1(a, b string) bool {
	if a == b {
		return true
	}
	switch {
	case len(a) == len(b):
		diff := 0
		for i := range a {
			if a[i] != b[i] {
				diff++
			}
		}
		return diff == 1
	case len(a) == len(b)+1:
		return editDistanceWithin1(b, a)
	case len(b) == len(a)+1:
		for i := range a {
			if a[i] != b[i] {
				return a[i:] == b[i+1:]
			}
		}
		return true
	}
	return false
}

// runScratchCommand implements `bulle scratch <verb> [id]`. The verbs mirror
// the review-prompt letters — list, diff, pull, wipe, shell — and are
// re-entrant views of the same review: a kept scratch (or one left behind by
// a failed pull) picks up exactly where the prompt left off.
func runScratchCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: bulle scratch list|diff|pull|wipe|shell [id]")
		fmt.Fprintln(stderr, "       bulle scratch [run flags] [workspace] [-- command [args...]]")
		return exitcode.ConfigError
	}
	verb := args[0]
	root, err := scratchCommandRoot()
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		return exitcode.ConfigError
	}
	scratches, err := listScratches(root)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		return exitcode.ConfigError
	}
	if verb == "list" {
		if len(args) > 1 {
			fmt.Fprintln(stderr, "usage: bulle scratch list")
			return exitcode.ConfigError
		}
		writeScratchList(scratches, stdout)
		return exitcode.OK
	}
	var id string
	switch len(args) {
	case 1:
	case 2:
		id = args[1]
	default:
		fmt.Fprintf(stderr, "usage: bulle scratch %s [id]\n", verb)
		return exitcode.ConfigError
	}
	s, err := selectScratch(scratches, id)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		return exitcode.ConfigError
	}
	switch verb {
	case "diff":
		final, err := worktreeTree(s.Dir)
		if err != nil {
			fmt.Fprintf(stderr, "bulle: %v\n", err)
			return exitcode.ConfigError
		}
		pageScratchDiff(s, final, stderr)
		return exitcode.OK
	case "pull":
		if scratchIsDirty(s) {
			fmt.Fprintf(stderr, "bulle: the scratch has uncommitted changes; pull only moves commits\n")
			fmt.Fprintf(stderr, "commit them first (bulle scratch shell %s), or wipe the scratch\n", s.ID)
			return exitcode.ConfigError
		}
		if !pullScratch(s, stdout, stderr) {
			return exitcode.CommandFailed
		}
		return exitcode.OK
	case "wipe":
		return wipeScratch(s, stdout, stderr)
	case "shell":
		openScratchShell(s, stdout, stderr)
		return exitcode.OK
	default:
		fmt.Fprintf(stderr, "bulle: unknown scratch command %q; use list, diff, pull, wipe, or shell\n", verb)
		return exitcode.ConfigError
	}
}

func wipeScratch(s *scratchState, stdout, stderr io.Writer) int {
	final, err := worktreeTree(s.Dir)
	changed := "unknown"
	if err == nil {
		lines, _, _, _ := scratchChangeSummary(s, final)
		changed = fmt.Sprintf("%d", len(lines))
	}
	if stdinIsTerminal() {
		fmt.Fprintf(stdout, "wipe %s (%s changed files)? [y/N] ", s.Dir, changed)
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(answer) != "y" {
			fmt.Fprintln(stdout, "not wiped")
			return exitcode.OK
		}
	}
	removeScratch(s)
	fmt.Fprintf(stdout, "wiped %s\n", s.Dir)
	return exitcode.OK
}

func writeScratchList(scratches []*scratchState, w io.Writer) {
	if len(scratches) == 0 {
		fmt.Fprintln(w, "no scratches")
		return
	}
	for _, s := range scratches {
		age := "?"
		if meta, err := readScratchMeta(s.Dir); err == nil && meta.Created != "" {
			if created, err := time.Parse(time.RFC3339, meta.Created); err == nil {
				age = formatAge(time.Since(created))
			}
		}
		summary := "?"
		if final, err := worktreeTree(s.Dir); err == nil {
			if final == s.BaselineTree && !scratchIsDirty(s) {
				summary = "no changes"
			} else {
				lines, added, deleted, changed := scratchChangeSummary(s, final)
				_ = lines
				summary = fmt.Sprintf("%d changed, %d added, %d deleted", changed, added, deleted)
			}
		}
		fmt.Fprintf(w, "%s  %s  %s  %s\n", s.ID, age, summary, s.Origin)
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// selectScratch picks a scratch by id, unique id prefix, or — with no id —
// the only scratch, preferring the only one whose origin is the current
// directory when several exist.
func selectScratch(scratches []*scratchState, id string) (*scratchState, error) {
	if len(scratches) == 0 {
		return nil, fmt.Errorf("no scratches; create one with bulle --scratch")
	}
	if id != "" {
		var matches []*scratchState
		for _, s := range scratches {
			if s.ID == id {
				return s, nil
			}
			if strings.HasPrefix(s.ID, id) {
				matches = append(matches, s)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("id %q is ambiguous; see bulle scratch list", id)
		}
		return nil, fmt.Errorf("no scratch with id %q; see bulle scratch list", id)
	}
	if len(scratches) == 1 {
		return scratches[0], nil
	}
	if cwd, err := os.Getwd(); err == nil {
		var matches []*scratchState
		for _, s := range scratches {
			if s.Origin == cwd {
				matches = append(matches, s)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
	}
	return nil, fmt.Errorf("multiple scratches; pass an id from bulle scratch list")
}

// listScratches reconstructs scratch state from the directories and their
// meta.toml files, newest first. Directories without readable metadata are
// skipped: they are either partial creations or not bulle's.
func listScratches(root string) ([]*scratchState, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*scratchState
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		meta, err := readScratchMeta(dir)
		if err != nil {
			continue
		}
		idx := strings.LastIndex(entry.Name(), "-")
		if idx < 0 {
			continue
		}
		out = append(out, &scratchState{
			Dir:          dir,
			Origin:       meta.Origin,
			ID:           entry.Name()[idx+1:],
			BaseCommit:   meta.BaseCommit,
			BaselineTree: meta.BaselineTree,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir > out[j].Dir })
	return out, nil
}

func readScratchMeta(dir string) (scratchMeta, error) {
	data, err := os.ReadFile(scratchMetaPath(dir))
	if err != nil {
		return scratchMeta{}, err
	}
	var meta scratchMeta
	if err := toml.Unmarshal(data, &meta); err != nil {
		return scratchMeta{}, err
	}
	return meta, nil
}

// scratchCommandRoot resolves the scratch directory the same way a --scratch
// run does, honoring [scratch] dir from the user configuration.
func scratchCommandRoot() (string, error) {
	global, err := loadConfig(cli.Options{})
	if err != nil {
		return "", err
	}
	root := scratchRoot(global.Scratch.Dir)
	if root == "" {
		return "", fmt.Errorf("could not determine the state directory")
	}
	return root, nil
}
