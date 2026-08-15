package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

// A recorder accumulates the grants a command was denied across rounds. It is
// threaded into the ordinary run path so recording observes exactly what a
// real run does, rather than a re-implementation of it.
type recorder struct {
	grants []grant
	seen   map[grant]bool
	// origins records which processes were denied each grant, where the
	// platform reports one. It annotates the output rather than filtering it.
	origins map[grant][]string
	// saved marks grants already written to a profile by the save prompt.
	saved map[grant]bool
	// lastAdded is how many grants the most recent round contributed. Zero
	// after a round that ran is the loop's stop condition.
	lastAdded int
	// lastObserved is how many denials the most recent round saw at all,
	// before deduplication and coverage filtering. It separates the two very
	// different ways a round can add nothing: the sandbox refused nothing, or
	// it refused only things already granted.
	lastObserved int
}

func newRecorder() *recorder {
	return &recorder{seen: map[grant]bool{}, origins: map[grant][]string{}, saved: map[grant]bool{}}
}

// unsaved returns the accumulated grants not yet written to a profile, in
// observation order.
func (r *recorder) unsaved() []grant {
	var out []grant
	for _, gr := range r.grants {
		if !r.saved[gr] {
			out = append(out, gr)
		}
	}
	return out
}

// markSaved records that every accumulated grant has been written, so a later
// prompt in the same session only shows what is new.
func (r *recorder) markSaved() {
	for _, gr := range r.grants {
		r.saved[gr] = true
	}
}

// beginRound clears the per-round counter, so a round that returns before the
// command ever runs — an invalid policy, a missing backend — reads as having
// learned nothing rather than inheriting the previous round's progress and
// spinning to the cap.
func (r *recorder) beginRound() { r.lastAdded, r.lastObserved = 0, 0 }

// observe collects the denials of one round, dropping those the round's own
// policy already granted, and reports how many were new. Zero means the round
// learned nothing: either the command succeeded, or it is failing for a reason
// no grant will fix.
func (r *recorder) observe(p policy.Policy, probe denialProbe) int {
	added := 0
	all := probe.grants()
	r.lastObserved = len(all)
	for _, observed := range filterCoveredGrants(all, p) {
		gr := observed.Grant
		if isProbeArtifact(gr.Path) {
			continue
		}
		r.noteOrigin(gr, observed.Origin)
		if r.seen[gr] {
			continue
		}
		r.seen[gr] = true
		r.grants = append(r.grants, gr)
		added++
	}
	r.lastAdded = added
	return added
}

// noteOrigin records a process that was denied a grant, keeping the list
// deduplicated and ordered by first sighting. A grant can be hit by several
// processes across rounds, and all of them are worth showing.
func (r *recorder) noteOrigin(gr grant, origin string) {
	if origin == "" {
		return
	}
	for _, existing := range r.origins[gr] {
		if existing == origin {
			return
		}
	}
	r.origins[gr] = append(r.origins[gr], origin)
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

// recordVars rebuilds the variable table the generalizer substitutes against.
// It mirrors what policy.Resolve computes for a run; the exported helper keeps
// the two from drifting apart.
func recordVars() map[string]string {
	home, _ := os.UserHomeDir()
	tmp := runtimeTempRoot(os.TempDir())
	cwd, _ := os.Getwd()
	return policy.RecordingVars(cwd, home, tmp, parentEnv())
}
