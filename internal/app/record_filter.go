package app

import (
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// coveredByPolicy reports whether a resolved policy already permits everything
// a grant asks for. Each access bit is checked against the lists that confer
// it, so a path granted read-only does not count as covering a denied execute.
//
// A denial implies the access was refused, so this is not merely a
// deduplication pass: it catches the cases where the denied path and the
// granted entry are the same place spelled differently — most often a symlink
// alias, since the kernel reports the resolved path while the profile grants
// the link.
func coveredByPolicy(gr grant, p policy.Policy) bool {
	want := accessForList(listForFlag(gr.Flag))
	if want == 0 {
		return false
	}
	readable := concat(p.ReadOnly, p.ReadOnlyExec, p.ReadWrite, p.ReadWriteExec)
	executable := concat(p.ReadOnlyExec, p.ReadWriteExec)
	writable := concat(p.ReadWrite, p.ReadWriteExec)

	if want&accessRead != 0 && !pathWithinRoots(gr.Path, readable) {
		return false
	}
	if want&accessExec != 0 && !pathWithinRoots(gr.Path, executable) {
		return false
	}
	if want&accessWrite != 0 && !pathWithinRoots(gr.Path, writable) {
		return false
	}
	return true
}

// pathWithinRoots reports whether any spelling of a path falls under a granted
// root. Every symlink variant is tried, not only the fully resolved one: the
// link and its target are equally valid ways to name the grant, and a profile
// is as likely to have written one as the other.
func pathWithinRoots(path string, roots []string) bool {
	if bpaths.IsWithinAnyRoot(path, roots) {
		return true
	}
	for _, variant := range bpaths.SymlinkPathVariants(path) {
		if bpaths.IsWithinAnyRoot(variant, roots) {
			return true
		}
	}
	return false
}

// filterCoveredGrants drops the grants a policy already permits, preserving
// order. Filtering happens on absolute paths, before generalization, so a
// covered denial never reaches the rewriting layer and never widens a grant to
// a resolver directory on the strength of an access that was already allowed.
func filterCoveredGrants(grants []grant, p policy.Policy) []grant {
	out := make([]grant, 0, len(grants))
	for _, gr := range grants {
		if coveredByPolicy(gr, p) {
			continue
		}
		out = append(out, gr)
	}
	return out
}

// grantsForDenials maps denial records to grants, dropping the records that
// name nothing grantable and collapsing exact duplicates. Order is the order
// the denials arrived, so the earliest occurrence of a repeated path wins and
// the result reads as a trace of the run.
func grantsForDenials(denials []landlockDenial) []grant {
	seen := map[grant]bool{}
	out := []grant{}
	for _, d := range denials {
		gr, ok := grantForDenial(d)
		if !ok || seen[gr] {
			continue
		}
		seen[gr] = true
		out = append(out, gr)
	}
	return out
}

// grantsForSeatbeltDenials is the macOS counterpart of grantsForDenials.
func grantsForSeatbeltDenials(denials []seatbeltDenial) []grant {
	seen := map[grant]bool{}
	out := []grant{}
	for _, d := range denials {
		gr, ok := grantForSeatbeltDenial(d)
		if !ok || seen[gr] {
			continue
		}
		seen[gr] = true
		out = append(out, gr)
	}
	return out
}

func concat(lists ...[]string) []string {
	total := 0
	for _, list := range lists {
		total += len(list)
	}
	out := make([]string, 0, total)
	for _, list := range lists {
		out = append(out, list...)
	}
	return out
}
