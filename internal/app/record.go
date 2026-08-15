package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

// defaultRecordRounds bounds the iteration. A Landlock denial aborts the
// operation that hit it, so each round only reveals the accesses the command
// reached before dying; a tool that fails early needs several passes. Ten is
// well past what any observed command has taken, and the cap being reached is
// reported rather than silently accepted.
const defaultRecordRounds = 10

// A recorder accumulates the grants a command was denied across rounds. It is
// threaded into the ordinary run path so recording observes exactly what a
// real run does, rather than a re-implementation of it.
type recorder struct {
	grants []grant
	seen   map[grant]bool
	// lastAdded is how many grants the most recent round contributed. Zero
	// after a round that ran is the loop's stop condition.
	lastAdded int
}

func newRecorder() *recorder { return &recorder{seen: map[grant]bool{}} }

// beginRound clears the per-round counter, so a round that returns before the
// command ever runs — an invalid policy, a missing backend — reads as having
// learned nothing rather than inheriting the previous round's progress and
// spinning to the cap.
func (r *recorder) beginRound() { r.lastAdded = 0 }

// observe collects the denials of one round, dropping those the round's own
// policy already granted, and reports how many were new. Zero means the round
// learned nothing: either the command succeeded, or it is failing for a reason
// no grant will fix.
func (r *recorder) observe(p policy.Policy, probe denialProbe) int {
	added := 0
	for _, gr := range filterCoveredGrants(probe.grants(), p) {
		if r.seen[gr] || isProbeArtifact(gr.Path) {
			continue
		}
		r.seen[gr] = true
		r.grants = append(r.grants, gr)
		added++
	}
	r.lastAdded = added
	return added
}

// probeDirPrefix names the temporary directories the denial-logging probe
// creates. See isProbeArtifact.
const probeDirPrefix = "bulle-probe-"

// isProbeArtifact reports whether a denied path is one the precondition probe
// caused rather than the observed command.
//
// The probe deliberately triggers a denial moments before the first round, and
// journalctl filters by whole seconds, so its record routinely lands inside
// round one's window. Without this the first recorded profile of every session
// would grant write access to a temporary file that no longer exists.
func isProbeArtifact(path string) bool {
	dir := filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(dir), probeDirPrefix) {
		return false
	}
	// Only inside a temporary directory: a real path that happens to sit in a
	// directory of that name is still the command's business.
	tmp := os.TempDir()
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}
	return strings.HasPrefix(dir, filepath.Clean(tmp)+string(filepath.Separator))
}

// flags renders the accumulated grants as command-line arguments, so the next
// round runs under a policy widened by exactly what the last one was denied.
func (r *recorder) flags() []string {
	out := make([]string, 0, len(r.grants)*2)
	for _, gr := range r.grants {
		out = append(out, gr.Flag, gr.Path)
	}
	return out
}

// recordOptions are the record-specific arguments, separated from the run
// arguments they wrap.
type recordOptions struct {
	base      string
	out       string
	maxRounds int
	runArgs   []string
}

func runRecordCommand(argv0 string, args []string, stdout io.Writer, stderr io.Writer) int {
	opts, err := parseRecordArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "bulle: %v\n", err)
		fmt.Fprintln(stderr, "usage: bulle record --profile <name> [--out <file>] [--max-rounds <n>] -- <command> [args...]")
		return ExitConfigError
	}
	if reason, ok := recordingSupported(); !ok {
		fmt.Fprintf(stderr, "bulle: %s\n", reason)
		return ExitConfigError
	}

	rec := newRecorder()
	baseArgs := append([]string{argv0, "--profile", opts.base}, opts.runArgs...)
	rounds, exit := 0, ExitOK
	stalled := false
	for rounds < opts.maxRounds {
		rounds++
		rec.beginRound()
		fmt.Fprintf(stderr, "bulle: record: round %d\n", rounds)
		// The command's own output goes to the terminal as it always does;
		// only bulle's narration is prefixed.
		exit = runMain(withGrantFlags(baseArgs, rec.flags()), "", stdout, stderr, rec)
		added := rec.lastAdded
		switch {
		case exit == ExitOK:
			fmt.Fprintf(stderr, "bulle: record: command succeeded; %d grants recorded over %d round(s)\n", len(rec.grants), rounds)
		case added == 0:
			stalled = true
			fmt.Fprintf(stderr, "bulle: record: round %d added no grants but the command still failed with exit %d\n", rounds, exit)
			fmt.Fprintln(stderr, "bulle: record: no grant will fix that; recording what was learned so far")
		default:
			fmt.Fprintf(stderr, "bulle: record: %d new grant(s); retrying\n", added)
		}
		if exit == ExitOK || added == 0 {
			break
		}
	}
	capped := rounds >= opts.maxRounds && exit != ExitOK && !stalled
	if capped {
		fmt.Fprintf(stderr, "bulle: record: stopped at the %d-round cap with the command still failing; the profile below is incomplete\n", opts.maxRounds)
	}

	profile := buildRecordedProfile(opts, rec, recordOutcome{
		rounds: rounds,
		exit:   exit,
		capped: capped,
	})
	if opts.out == "" {
		fmt.Fprint(stdout, profile)
	} else if err := os.WriteFile(opts.out, []byte(profile), 0o644); err != nil {
		fmt.Fprintf(stderr, "bulle: record: %v\n", err)
		return ExitConfigError
	} else {
		fmt.Fprintf(stderr, "bulle: record: wrote %s\n", opts.out)
	}
	// Recording succeeded as a recording even when the command it observed did
	// not, so the exit code reports the recording. The header says what the
	// command did.
	return ExitOK
}

// withGrantFlags inserts the accumulated grants ahead of the run arguments.
// They go first so a "--" in the user's arguments still separates the command.
func withGrantFlags(baseArgs []string, flags []string) []string {
	out := make([]string, 0, len(baseArgs)+len(flags))
	out = append(out, baseArgs[0])
	out = append(out, flags...)
	out = append(out, baseArgs[1:]...)
	return out
}

func parseRecordArgs(args []string) (recordOptions, error) {
	opts := recordOptions{maxRounds: defaultRecordRounds}
	i := 0
	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", arg)
			}
			i++
			return args[i], nil
		}
		var err error
		switch arg {
		case "--profile", "-p":
			opts.base, err = value()
		case "--out", "-o":
			opts.out, err = value()
		case "--max-rounds":
			var raw string
			if raw, err = value(); err == nil {
				opts.maxRounds, err = strconv.Atoi(raw)
				if err == nil && opts.maxRounds < 1 {
					err = fmt.Errorf("--max-rounds must be at least 1")
				}
			}
		default:
			// Anything else begins the run arguments. Recording wraps an
			// ordinary run, so the rest is passed through untouched.
			opts.runArgs = args[i:]
			i = len(args)
		}
		if err != nil {
			return recordOptions{}, err
		}
	}
	if i < len(args) {
		opts.runArgs = append(opts.runArgs, args[i:]...)
	}
	if opts.base == "" {
		return recordOptions{}, fmt.Errorf("record needs a base profile: pass --profile <name>")
	}
	if len(opts.runArgs) == 0 {
		return recordOptions{}, fmt.Errorf("record needs a command to observe")
	}
	return opts, nil
}

type recordOutcome struct {
	rounds int
	exit   int
	capped bool
}

// buildRecordedProfile renders the accumulated grants as a profile that
// inherits from the base. The header is part of the output rather than
// something the docs say elsewhere: a recorded profile is evidence that one
// run of one command on one machine needed these grants, and whoever reads it
// later should be told exactly that.
func buildRecordedProfile(opts recordOptions, rec *recorder, outcome recordOutcome) string {
	g := newGeneralizer(recordVars(), policy.ListResolvers(os.Getenv("PATH"), parentEnv()), exec.LookPath)
	entries := make([]recordedEntry, 0, len(rec.grants))
	for _, gr := range rec.grants {
		entries = append(entries, g.generalize(gr))
	}
	entries = promoteDirectories(mergeEntries(entries))

	var b strings.Builder
	fmt.Fprintf(&b, "# Recorded by bulle %s on %s.\n", Version, time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "# Command: %s\n", strings.Join(opts.runArgs, " "))
	fmt.Fprintf(&b, "# Rounds: %d; command exited %d.\n", outcome.rounds, outcome.exit)
	b.WriteString("#\n")
	b.WriteString("# This records what one run needed, not what the command needs. A denial\n")
	b.WriteString("# aborts the operation that hit it, so anything the command did not reach\n")
	b.WriteString("# is missing here — optional features, error paths, and anything behind a\n")
	b.WriteString("# flag this run did not pass. Review every entry before installing it.\n")
	if outcome.capped {
		b.WriteString("#\n# INCOMPLETE: recording stopped at the round cap with the command still\n")
		b.WriteString("# failing. Re-run with a higher --max-rounds.\n")
	}
	if outcome.exit != ExitOK && !outcome.capped {
		b.WriteString("#\n# The command failed for a reason no grant fixed. These entries may be\n")
		b.WriteString("# necessary but they are demonstrably not sufficient.\n")
	}
	fmt.Fprintf(&b, "\ntitle = %q\n", "Recorded: "+opts.base)
	fmt.Fprintf(&b, "description = %q\n", "recorded from "+strings.Join(opts.runArgs, " "))
	fmt.Fprintf(&b, "inherits = [%q]\n", opts.base)

	if len(entries) == 0 {
		b.WriteString("\n# The run was denied nothing the base profile did not already grant.\n")
		return b.String()
	}
	for _, list := range []string{"ro", "rox", "rw", "rwx"} {
		writeRecordedList(&b, list, entries)
	}
	return b.String()
}

func writeRecordedList(b *strings.Builder, list string, entries []recordedEntry) {
	var rows []recordedEntry
	for _, e := range entries {
		if e.List == list {
			rows = append(rows, e)
		}
	}
	if len(rows) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Entry < rows[j].Entry })
	fmt.Fprintf(b, "\n%s = [\n", list)
	for _, row := range rows {
		if row.Comment != "" {
			fmt.Fprintf(b, "  # %s\n", row.Comment)
		}
		// Every recorded entry is optional. A path this machine has is not a
		// path the next one has, and a profile that fails to resolve is worse
		// than one that grants nothing.
		fmt.Fprintf(b, "  %q,\n", "?"+row.Entry)
	}
	b.WriteString("]\n")
}

// recordVars rebuilds the variable table the generalizer substitutes against.
// It mirrors what policy.Resolve computes for a run; the exported helper keeps
// the two from drifting apart.
func recordVars() map[string]string {
	home, _ := os.UserHomeDir()
	tmp := runtimeTempRoot(os.TempDir())
	cwd, _ := os.Getwd()
	return policy.RecordingVars(cwd, home, tmp, parentEnv())
}
