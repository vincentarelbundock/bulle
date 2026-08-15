// Package record turns sandbox denials into profile entries.
//
// A run hands it the policy that was in effect and a Probe over the platform's
// denial log; it parses what was refused (Landlock records on Linux, Seatbelt
// on macOS), drops what the policy already granted, generalizes the rest into
// portable entries — variables substituted, package-store roots collapsed,
// crowded directories promoted — and offers to save them to a profile.
//
// It lives outside internal/app so the command layer only wires it up, and so
// this logic can be tested without the CLI around it.
package record

import (
	"os"
	"path/filepath"
	"strings"

	benv "github.com/vincentarelbundock/bulle/internal/env"
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// A Recorder accumulates the grants a command was denied across rounds. It is
// threaded into the ordinary run path so recording observes exactly what a
// real run does, rather than a re-implementation of it.
type Recorder struct {
	grants []Grant
	seen   map[Grant]bool
	// origins records which processes were denied each Grant, where the
	// platform reports one. It annotates the output rather than filtering it.
	origins map[Grant][]string
	// saved marks grants already written to a profile by the save prompt.
	saved map[Grant]bool
	// LastAdded is how many grants the most recent round contributed. Zero
	// after a round that ran is the loop's stop condition.
	LastAdded int
	// LastObserved is how many denials the most recent round saw at all,
	// before deduplication and coverage filtering. It separates the two very
	// different ways a round can add nothing: the sandbox refused nothing, or
	// it refused only things already granted.
	LastObserved int
}

func NewRecorder() *Recorder {
	return &Recorder{seen: map[Grant]bool{}, origins: map[Grant][]string{}, saved: map[Grant]bool{}}
}

// Unsaved returns the accumulated grants not yet written to a profile, in
// observation order.
func (r *Recorder) Unsaved() []Grant {
	var out []Grant
	for _, gr := range r.grants {
		if !r.saved[gr] {
			out = append(out, gr)
		}
	}
	return out
}

// MarkSaved records that every accumulated Grant has been written, so a later
// prompt in the same session only shows what is new.
func (r *Recorder) MarkSaved() {
	for _, gr := range r.grants {
		r.saved[gr] = true
	}
}

// BeginRound clears the per-round counter, so a round that returns before the
// command ever runs — an invalid policy, a missing backend — reads as having
// learned nothing rather than inheriting the previous round's progress and
// spinning to the cap.
func (r *Recorder) BeginRound() { r.LastAdded, r.LastObserved = 0, 0 }

// observe collects the denials of one round, dropping those the round's own
// policy already granted, and reports how many were new. Zero means the round
// learned nothing: either the command succeeded, or it is failing for a reason
// no Grant will fix.
func (r *Recorder) Observe(p policy.Policy, probe Probe) int {
	added := 0
	all := probe.Grants()
	r.LastObserved = len(all)
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
	r.LastAdded = added
	return added
}

// noteOrigin records a process that was denied a Grant, keeping the list
// deduplicated and ordered by first sighting. A Grant can be hit by several
// processes across rounds, and all of them are worth showing.
func (r *Recorder) noteOrigin(gr Grant, origin string) {
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
// would Grant write access to a temporary file that no longer exists.
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
	tmp := bpaths.RuntimeTempRoot(os.TempDir())
	cwd, _ := os.Getwd()
	return policy.RecordingVars(cwd, home, tmp, benv.Parent())
}
