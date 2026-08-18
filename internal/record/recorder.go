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

	benv "github.com/vincentarelbundock/bulle/internal/env"
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// A Recorder accumulates the grants a command was denied during a run. It is
// threaded into the ordinary run path so recording observes exactly what a
// real run does, rather than a re-implementation of it.
type Recorder struct {
	grants []Grant
	seen   map[Grant]bool
	// origins records which processes were denied each grant, where the
	// platform reports one. It annotates the output rather than filtering it.
	origins map[Grant][]string
	// LastAdded is how many grants the most recent round contributed. Zero
	// after a round that ran means the run needs no grant it does not have.
	LastAdded int
	// LastObserved is how many denials the most recent round saw at all,
	// before deduplication and coverage filtering. It separates the two very
	// different ways a round can add nothing: the sandbox refused nothing, or
	// it refused only things already granted.
	LastObserved int
}

func NewRecorder() *Recorder {
	return &Recorder{seen: map[Grant]bool{}, origins: map[Grant][]string{}}
}

// BeginRound clears the per-round counter, so a run that returns before the
// command ever runs — an invalid policy, a missing backend — reads as having
// observed nothing rather than reporting the previous round's grants a second
// time.
func (r *Recorder) BeginRound() { r.LastAdded, r.LastObserved = 0, 0 }

// Observe collects the denials of one round, dropping those the round's own
// policy already granted, and reports how many were new. Zero means the round
// learned nothing: either the command succeeded, or it is failing for a reason
// no grant will fix.
func (r *Recorder) Observe(p policy.Policy, probe Probe) int {
	added := 0
	all := probe.Grants()
	r.LastObserved = len(all)
	for _, observed := range filterCoveredGrants(all, p) {
		gr := observed.Grant
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

// noteOrigin records a process that was denied a grant, keeping the list
// deduplicated and ordered by first sighting. A grant can be hit by several
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

// recordVars rebuilds the variable table the generalizer substitutes against.
// It mirrors what policy.Resolve computes for a run; the exported helper keeps
// the two from drifting apart.
func recordVars() map[string]string {
	home, _ := os.UserHomeDir()
	tmp := bpaths.RuntimeTempRoot(os.TempDir())
	cwd, _ := os.Getwd()
	return policy.RecordingVars(cwd, home, tmp, benv.Parent())
}
