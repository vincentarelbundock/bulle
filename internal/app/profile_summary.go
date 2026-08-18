package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/limits"
	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func shouldPrintProfileSummary(opts cli.Options) bool {
	return opts.Profile != "" && !opts.Policy
}

func writeProfilePermissionSummary(profileName string, p policy.Policy, w io.Writer, shortenStore bool) {
	view := policy.NewView(p)
	paths := newPathSummaryFormatter(p)
	paths.shortenStore = shortenStore
	fmt.Fprintf(w, "bulle profile %q permissions:\n", profileName)
	fmt.Fprintf(w, "  backend: %s\n", view.Backend)
	fmt.Fprintf(w, "  command: %s\n", formatCommand(view.Command))
	fmt.Fprintf(w, "  workspace: %s\n", paths.formatProject(view.ProjectPath))
	fmt.Fprintln(w, "  filesystem:")
	writePermissionGroup(w, "ro", view.ReadOnly, paths, summaryRO)
	writePermissionGroup(w, "rox", view.ReadOnlyExec, paths, summaryROX)
	writePermissionGroup(w, "rw", view.ReadWrite, paths, summaryRW)
	writePermissionGroup(w, "rwx", view.ReadWriteExec, paths, summaryRWX)
	fmt.Fprintf(w, "  environment: %s\n", formatInlineList(view.EnvKeys))
	fmt.Fprintf(w, "  network: %s\n", formatNetwork(view.Network))
	if view.Timeout != "" {
		fmt.Fprintf(w, "  timeout: %s\n", view.Timeout)
	}
	writeLimits(w, view.Limits)
	// With a command, add_exec and add_libs have already materialized into the
	// filesystem lists above; the flags add nothing. Without one, the lists
	// under-state a real run, so say what would be added.
	if len(view.Command) == 0 {
		if note := formatDiscoveryNote(view.AddExec, view.AddLibs); note != "" {
			fmt.Fprintf(w, "  note: %s\n", note)
		}
	}
	if len(view.MachLookup) > 0 {
		fmt.Fprintf(w, "  mach_lookup: %s\n", formatInlineList(view.MachLookup))
	}
}

// writeLimits reports the requested resource limits with the mechanism behind
// each one. The mechanism is shown rather than implied because "memory: 4G"
// means something materially different when a cgroup is counting resident
// memory than when nothing is counting at all, and the difference varies by
// platform and by whether this host delegates a cgroup.
func writeLimits(w io.Writer, statuses []limits.Status) {
	if len(statuses) == 0 {
		return
	}
	fmt.Fprintln(w, "  limits:")
	width := 0
	for _, status := range statuses {
		width = max(width, len(status.Name)+1)
	}
	for _, status := range statuses {
		enforcement := string(status.Mechanism)
		if !status.Enforced {
			enforcement = "NOT ENFORCED — " + status.Reason
		}
		fmt.Fprintf(w, "    %-*s %s  (%s)\n", width, status.Name+":", status.Value, enforcement)
	}
}

func formatDiscoveryNote(addExec bool, addLibs bool) string {
	switch {
	case addExec && addLibs:
		return "a real run also grants the command executable and its runtime libraries (add_exec, add_libs)"
	case addExec:
		return "a real run also grants the command executable (add_exec)"
	case addLibs:
		return "a real run also grants the command's runtime libraries (add_libs)"
	default:
		return ""
	}
}

// writeResolutionTable prints configured entries whose resolution needs
// attention: skipped, created, or granted with a wider effective right through
// another list. Entries granted as specified restate the summary above and
// are collapsed into a count; --policy=json holds the full mapping. Shown
// only for --policy: "why can't the agent see X" is answerable from one
// command.
func writeResolutionTable(p policy.Policy, w io.Writer) {
	if len(p.Trace) == 0 {
		return
	}
	rights := resolvedRights(p)
	granted, skipped := 0, 0
	lines := []string{}
	for _, trace := range p.Trace {
		note := unionNote(trace, rights)
		if note == "" && strings.HasPrefix(trace.Outcome, "granted") {
			granted++
			continue
		}
		// A skipped entry grants nothing: it is absent from the actual access
		// above, so it only earns a line in the count.
		if note == "" && strings.HasPrefix(trace.Outcome, "skipped") {
			skipped++
			continue
		}
		line := fmt.Sprintf("    %-4s %-40s → %s", trace.List, trace.Raw, formatTraceOutcome(trace))
		if note != "" {
			line += " " + note
		}
		lines = append(lines, line)
	}
	fmt.Fprintln(w, "  resolution:")
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "    %d entries granted, %d skipped as absent; --policy=json lists every entry\n", granted, skipped)
}

// writeResolverListing prints every known resolver next to what it resolves to
// on this machine. A resolver that reports nothing is otherwise
// indistinguishable from one that works, so unavailable and empty results are
// listed as loudly as successful ones.
func writeResolverListing(env map[string]string, w io.Writer) {
	fmt.Fprintln(w, "Executable resolvers (rox and rwx lists only; NAME is a command you supply):")
	fmt.Fprintln(w, "  which:NAME       the binary named NAME on PATH, plus a shim entry")
	fmt.Fprintln(w, "  pkg:NAME         the same, plus the package tree above the real binary")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Tool resolvers (valid in every list; the aspect is one this tool knows):")
	for _, listing := range policy.ListResolvers(env["PATH"], env) {
		fmt.Fprintf(w, "  %-16s %s\n", listing.Entry, listing.Description)
		if len(listing.Paths) == 0 {
			fmt.Fprintf(w, "  %-16s → %s\n", "", listing.Outcome)
			continue
		}
		fmt.Fprintf(w, "  %-16s → %s\n", "", listing.Paths[0])
		// A resolver can legitimately report dozens of paths (R's search path
		// runs to 70 entries on a Nix machine); show enough to recognize the
		// shape without burying the rest of the listing.
		const shown = 4
		for i, path := range listing.Paths[1:] {
			if i >= shown {
				fmt.Fprintf(w, "  %-16s   (+%d more; see --policy for the full list)\n", "", len(listing.Paths)-1-shown)
				break
			}
			fmt.Fprintf(w, "  %-16s   %s\n", "", path)
		}
	}
}

func formatTraceOutcome(trace bpaths.Trace) string {
	if len(trace.Paths) == 0 {
		return trace.Outcome
	}
	out := shortenStorePath(trace.Paths[0])
	if trace.Outcome != "granted" {
		out = trace.Outcome + " " + out
	}
	if extra := len(trace.Paths) - 1; extra > 0 {
		out += fmt.Sprintf(" (+%d more)", extra)
	}
	return out
}

// unionNote flags entries whose resolved path also appears in a stronger
// list: two portable entries can collapse to one path on a given platform
// (e.g. $CONFIG and $DATA on macOS), silently widening the weaker grant.
func unionNote(trace bpaths.Trace, rights map[string]string) string {
	for _, path := range trace.Paths {
		if effective, ok := rights[path]; ok && effective != trace.List {
			return fmt.Sprintf("(effective %s: also granted via another list)", effective)
		}
	}
	return ""
}

func resolvedRights(p policy.Policy) map[string]string {
	out := map[string]string{}
	// Strongest last so overlapping paths report their widest grant.
	for _, group := range []struct {
		label string
		paths []string
	}{
		{"ro", p.ReadOnly}, {"rox", p.ReadOnlyExec}, {"rw", p.ReadWrite}, {"rwx", p.ReadWriteExec},
	} {
		for _, path := range group.paths {
			out[path] = group.label
		}
	}
	return out
}

// Right bits for display-time subsumption checks; strongest last so
// overlapping grants keep their widest right.
const (
	summaryRead  = 1 << 0
	summaryExec  = 1 << 1
	summaryWrite = 1 << 2

	summaryRO  = summaryRead
	summaryROX = summaryRead | summaryExec
	summaryRW  = summaryRead | summaryWrite
	summaryRWX = summaryRead | summaryWrite | summaryExec
)

func writePermissionGroup(w io.Writer, label string, values []string, paths pathSummaryFormatter, rights int) {
	if len(values) == 0 {
		fmt.Fprintf(w, "    %s: none\n", label)
		return
	}
	writeWrappedItems(w, fmt.Sprintf("    %s: ", label), "        ", paths.formatList(values, rights))
}

type pathSummaryFormatter struct {
	project      string
	home         string
	tmp          string
	shortenStore bool
	granted      map[string][]int
}

func newPathSummaryFormatter(p policy.Policy) pathSummaryFormatter {
	// Each list a path appears in is recorded separately rather than unioned.
	// A path granted both rw and rox is covered by neither of them: unioning
	// them into rwx would make each list look subsumed by "the other", and the
	// path would be dropped from both — printing "none" over a grant that is in
	// fact read, write, and execute.
	granted := map[string][]int{}
	for _, group := range []struct {
		rights int
		paths  []string
	}{
		{summaryRO, p.ReadOnly}, {summaryROX, p.ReadOnlyExec}, {summaryRW, p.ReadWrite}, {summaryRWX, p.ReadWriteExec},
	} {
		for _, path := range group.paths {
			clean := cleanPath(path)
			if clean == "" {
				continue
			}
			if !containsInt(granted[clean], group.rights) {
				granted[clean] = append(granted[clean], group.rights)
			}
		}
	}
	return pathSummaryFormatter{
		project: cleanPath(p.ProjectPath),
		home:    cleanPath(p.Env["HOME"]),
		tmp:     cleanPath(firstSet(p.Env["TMP"], p.Env["TMPDIR"], p.Env["TEMP"], p.Env["BUN_TMPDIR"])),
		granted: granted,
	}
}

func (f pathSummaryFormatter) formatProject(path string) string {
	return f.formatPath(path, false)
}

func (f pathSummaryFormatter) formatList(values []string, rights int) []string {
	kept := f.dropSubsumed(values, rights)
	kept, aliases := collapseSymlinkAliases(kept)
	collapsed := f.collapsePrivateAliases(kept)
	grouped := groupSiblingPaths(collapsed)
	if len(grouped) == 0 {
		return []string{"none"}
	}
	if aliases > 0 {
		grouped = append(grouped, fmt.Sprintf("(+%d symlink aliases)", aliases))
	}
	return grouped
}

// dropSubsumed removes entries already covered by another grant: an exact
// duplicate in a strictly stronger list (/dev/null in both ro and rw), or a
// non-symlink path whose ancestor directory is granted with at least the same
// rights. A child that is itself a symlink stays: a granted path carries its
// symlink target with it, while a directory grant does not cover symlink
// targets inside it.
func (f pathSummaryFormatter) dropSubsumed(values []string, rights int) []string {
	out := []string{}
	for _, value := range values {
		clean := cleanPath(value)
		if clean == "" || !strings.HasPrefix(clean, "/") {
			out = append(out, value)
			continue
		}
		if coveredByStrongerGrant(f.granted[clean], rights) {
			continue
		}
		subsumed := false
		if info, err := os.Lstat(clean); err == nil && info.Mode()&os.ModeSymlink == 0 {
			for dir := filepath.Dir(clean); ; dir = filepath.Dir(dir) {
				if coveredByGrant(f.granted[dir], rights) {
					subsumed = true
					break
				}
				if dir == "/" || dir == "." {
					break
				}
			}
		}
		if !subsumed {
			out = append(out, value)
		}
	}
	return out
}

// coveredByStrongerGrant reports whether one single other grant on the same
// path permits everything this list does and more.
func coveredByStrongerGrant(held []int, rights int) bool {
	for _, r := range held {
		if r != rights && r&rights == rights {
			return true
		}
	}
	return false
}

// coveredByGrant reports whether one single grant permits everything this list
// does. Unlike coveredByStrongerGrant it accepts an equal grant, because the
// path being tested is a descendant rather than the same path.
func coveredByGrant(held []int, rights int) bool {
	for _, r := range held {
		if r&rights == rights {
			return true
		}
	}
	return false
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// collapseSymlinkAliases keeps one path per distinct filesystem object: a
// which: grant carries its whole symlink chain, and on layered systems
// (NixOS, home-manager) the same binary appears under half a dozen alias
// directories. The first spelling wins; the rest are counted, since they name
// the same access.
func collapseSymlinkAliases(values []string) ([]string, int) {
	seen := map[string]bool{}
	out := []string{}
	dropped := 0
	for _, value := range values {
		key := cleanPath(value)
		if resolved, err := filepath.EvalSymlinks(key); err == nil {
			key = resolved
		}
		if seen[key] {
			dropped++
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out, dropped
}

func (f pathSummaryFormatter) collapsePrivateAliases(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if clean := cleanPath(value); clean != "" {
			seen[clean] = true
		}
	}

	out := []string{}
	added := map[string]bool{}
	for _, value := range values {
		clean := cleanPath(value)
		if clean == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(clean, "/private/"); ok && seen["/"+rest] {
			continue
		}

		display := f.formatPath(clean, true)
		if seen["/private"+clean] {
			display += " (+ /private alias)"
		}
		if !added[display] {
			added[display] = true
			out = append(out, display)
		}
	}
	return out
}

func (f pathSummaryFormatter) formatPath(path string, aliasProject bool) string {
	clean := cleanPath(path)
	if clean == "" {
		return path
	}
	if aliasProject && f.project != "" {
		if clean == f.project {
			return "$WORKSPACE"
		}
		if strings.HasPrefix(clean, f.project+"/") {
			return "$WORKSPACE/" + strings.TrimPrefix(clean, f.project+"/")
		}
	}
	if f.tmp != "" {
		if clean == f.tmp {
			return "$TMP"
		}
		if strings.HasPrefix(clean, f.tmp+"/") {
			return "$TMP/" + strings.TrimPrefix(clean, f.tmp+"/")
		}
	}
	if suffix, ok := strings.CutSuffix(clean, "/bulle/tmp"); ok && looksLikeTempRoot(suffix) {
		return "$TMP/bulle/tmp"
	}
	if f.home != "" {
		if clean == f.home {
			return "~"
		}
		if strings.HasPrefix(clean, f.home+"/") {
			return "~/" + strings.TrimPrefix(clean, f.home+"/")
		}
	}
	if f.shortenStore {
		return shortenStorePath(clean)
	}
	return clean
}

// shortenStorePath abbreviates the 32-character hash in a Nix store path for
// display (/nix/store/y4nr67a…-hosts). A 7-character prefix keeps distinct
// items distinguishable; display-only, never used where a real path is needed.
func shortenStorePath(path string) string {
	rest, ok := strings.CutPrefix(path, "/nix/store/")
	if !ok || len(rest) < 33 || rest[32] != '-' {
		return path
	}
	for _, r := range rest[:32] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z') {
			return path
		}
	}
	return "/nix/store/" + rest[:7] + "…" + rest[32:]
}

func groupSiblingPaths(values []string) []string {
	type group struct {
		names []string
		seen  map[string]bool
	}
	groups := map[string]*group{}
	for _, value := range values {
		parent, name, ok := groupablePath(value)
		if !ok {
			continue
		}
		g, ok := groups[parent]
		if !ok {
			g = &group{seen: map[string]bool{}}
			groups[parent] = g
		}
		if g.seen[name] {
			continue
		}
		g.seen[name] = true
		g.names = append(g.names, name)
	}

	out := []string{}
	usedParents := map[string]bool{}
	for _, value := range values {
		parent, _, ok := groupablePath(value)
		if ok && len(groups[parent].names) > 1 {
			if usedParents[parent] {
				continue
			}
			usedParents[parent] = true
			out = append(out, parent+"/{"+strings.Join(groups[parent].names, ",")+"}")
			continue
		}
		out = append(out, value)
	}
	return out
}

func groupablePath(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " (+ ") {
		return "", "", false
	}
	parent, name := filepath.Split(value)
	parent = strings.TrimSuffix(parent, string(filepath.Separator))
	if parent == "" || parent == string(filepath.Separator) || strings.ContainsAny(name, "{},") {
		return "", "", false
	}
	return parent, name, true
}

func writeWrappedItems(w io.Writer, firstIndent string, nextIndent string, values []string) {
	const maxWidth = 100
	line := firstIndent
	for i, value := range values {
		sep := ""
		if i > 0 {
			sep = ", "
		}
		if i > 0 && len(line)+len(sep)+len(value) > maxWidth {
			fmt.Fprintln(w, line+",")
			line = nextIndent + value
			continue
		}
		line += sep + value
	}
	fmt.Fprintln(w, line)
}

func firstSet(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func looksLikeTempRoot(path string) bool {
	if isBaseTempRoot(path) {
		return true
	}
	base := filepath.Base(path)
	parent := filepath.Dir(path)
	return strings.HasPrefix(base, "bulle-") && isBaseTempRoot(parent)
}

func isBaseTempRoot(path string) bool {
	return strings.HasPrefix(path, "/var/folders/") || strings.HasPrefix(path, "/private/var/folders/") || path == "/tmp" || path == "/private/tmp"
}

func formatInlineList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func formatCommand(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, formatCommandArg(value))
	}
	return strings.Join(out, " ")
}

func formatCommandArg(value string) string {
	if value == "" {
		return strconv.Quote(value)
	}
	for _, r := range value {
		if !isSafeCommandArgRune(r) {
			return strconv.Quote(value)
		}
	}
	return value
}

func isSafeCommandArgRune(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '@', '%', '_', '+', '=', ':', ',', '.', '/', '-':
		return true
	default:
		return false
	}
}

func formatNetwork(value policy.NetworkMode) string {
	if value == "" {
		return string(policy.NetworkFull)
	}
	return string(value)
}

func formatEnabled(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}
