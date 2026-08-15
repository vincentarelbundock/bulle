package record

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// A recordedEntry is one line of a recorded profile: the list it belongs to,
// the entry as it should be written, and an optional comment explaining a
// choice a reader would otherwise have to reverse-engineer.
//
// Entry is the generalized spelling — "$CACHE/foo", "which:pandoc",
// "go:modcache" — not the path that was denied. The literal path is kept in
// Denied so the emitter can show what the entry was derived from.
type recordedEntry struct {
	List    string // "ro", "rox", "rw", "rwx"
	Entry   string
	Denied  string
	Comment string
}

// listForFlag maps a grant's command-line flag to the profile list name.
func listForFlag(flag string) string { return strings.TrimPrefix(flag, "--") }

// access is a grant's access level as a set of bits, so grants for the same
// path can be merged into the weakest entry that covers all of them.
type access uint8

const (
	accessRead access = 1 << iota
	accessWrite
	accessExec
)

func accessForList(list string) access {
	switch list {
	case "ro":
		return accessRead
	case "rox":
		return accessRead | accessExec
	case "rw":
		return accessRead | accessWrite
	case "rwx":
		return accessRead | accessWrite | accessExec
	}
	return 0
}

func listForAccess(a access) string {
	switch {
	case a&accessWrite != 0 && a&accessExec != 0:
		return "rwx"
	case a&accessExec != 0:
		return "rox"
	case a&accessWrite != 0:
		return "rw"
	default:
		return "ro"
	}
}

// A substitution rewrites a machine-specific path prefix into the portable
// spelling a profile should use.
type substitution struct {
	// Path is the absolute path this substitution matches, on this machine.
	Path string
	// Entry is what to write instead: a variable reference ("$CACHE") that
	// takes a suffix, or a resolver entry ("go:modcache") that does not.
	Entry string
	// Resolver marks an entry that names a whole tool-owned directory.
	// Resolver entries are the entire entry — profile syntax has no
	// "go:modcache/subdir" spelling — so a denial inside one is granted by
	// naming the directory itself.
	Resolver bool
}

// A generalizer rewrites denied paths into portable profile entries. It is
// built once per recording session from this machine's variable table and
// resolver registry, and holds no state across calls.
type generalizer struct {
	subs []substitution
	// lookPath resolves a bare command name the way the parent PATH would,
	// for recognizing that a denied executable is simply "the pandoc on PATH".
	// Injected so the mapping is testable without a populated PATH.
	lookPath func(name string) (string, error)
}

// newGeneralizer builds the substitution table. Longer paths sort first, so
// the most specific spelling wins: a path under the Go module cache becomes
// go:modcache rather than $HOME/go/pkg/mod.
func newGeneralizer(vars bpaths.Vars, resolvers []policy.ResolverListing, lookPath func(string) (string, error)) *generalizer {
	var subs []substitution
	for name, value := range vars {
		if !usableSubstitutionPath(value) {
			continue
		}
		subs = append(subs, substitution{Path: filepath.Clean(value), Entry: "$" + name})
	}
	for _, listing := range resolvers {
		if listing.Outcome != "ok" {
			continue
		}
		for _, path := range listing.Paths {
			if !usableSubstitutionPath(path) {
				continue
			}
			subs = append(subs, substitution{Path: filepath.Clean(path), Entry: listing.Entry, Resolver: true})
		}
	}
	sort.Slice(subs, func(i, j int) bool { return preferSubstitution(subs[i], subs[j]) })
	return &generalizer{subs: subs, lookPath: lookPath}
}

// usableSubstitutionPath rejects candidates too broad to be worth matching.
// The filesystem root would match everything, and a relative or empty value is
// not a prefix of any denied path. $HOME is deliberately kept: it is a useful
// prefix even though granting it whole never is, and only ever appears here
// with a suffix attached.
func usableSubstitutionPath(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	return filepath.Clean(path) != string(filepath.Separator)
}

// preferSubstitution orders the table: longest path first, then resolver
// entries ahead of variables at equal length, since a resolver names what the
// directory is for rather than merely where it sits. Remaining ties break on
// the spelling a hand-written profile would use ($CONFIG over
// $XDG_CONFIG_HOME, $TMP over $TMPDIR) and finally on name, so the table is
// deterministic despite being built from a map.
func preferSubstitution(a, b substitution) bool {
	if len(a.Path) != len(b.Path) {
		return len(a.Path) > len(b.Path)
	}
	if a.Resolver != b.Resolver {
		return a.Resolver
	}
	if aXDG, bXDG := strings.HasPrefix(a.Entry, "$XDG_"), strings.HasPrefix(b.Entry, "$XDG_"); aXDG != bXDG {
		return bXDG
	}
	if len(a.Entry) != len(b.Entry) {
		return len(a.Entry) < len(b.Entry)
	}
	return a.Entry < b.Entry
}

// generalize turns one grant into the profile entry that should be written
// for it.
func (g *generalizer) generalize(gr Grant) recordedEntry {
	list := listForFlag(gr.Flag)
	path := filepath.Clean(gr.Path)
	entry := recordedEntry{List: list, Denied: path}

	if path == "/proc" {
		// Collapsed from a per-process entry. This grants more than the denial
		// asked for and cannot be narrowed — see procGrantPath — so the entry
		// carries the tradeoff rather than leaving a reviewer to infer it.
		entry.Entry = path
		entry.Comment = "a per-process /proc entry was denied; only a whole-/proc Grant covers it," +
			" since the pid differs for every child. This lets the sandboxed command read" +
			" other same-uid processes' /proc entries."
		return entry
	}
	if name, ok := g.commandName(path, list); ok {
		entry.Entry = "which:" + name
		entry.Comment = "resolved from PATH; " + path + " here"
		return entry
	}
	for _, sub := range g.subs {
		rest, ok := pathUnder(path, sub.Path)
		if !ok {
			continue
		}
		switch {
		case sub.Resolver:
			// Naming the tool directory grants more than the one denied file.
			// That is the idiom the hand-written profiles already use, because
			// a tool's cache is used as a whole, but it is a widening and the
			// comment says so.
			entry.Entry = sub.Entry
			if rest != "" {
				entry.Comment = "for " + path
			}
			return entry
		case rest == "":
			// A bare variable root is never a grant worth writing: $HOME and
			// the app directories are exactly the trees a sandbox exists to
			// withhold. Stop here rather than continuing down the table, or a
			// broader variable would spell the same root a different way and
			// smuggle it back in as "$HOME/.cache".
			entry.Entry = path
			return entry
		default:
			entry.Entry = sub.Entry + "/" + rest
			return entry
		}
	}
	entry.Entry = path
	return entry
}

// commandName reports the bare command name for an executable grant whose
// denied path is what the parent PATH resolves that name to. Recording
// "which:pandoc" instead of one machine's pandoc path is the difference
// between a profile that travels and one that does not.
func (g *generalizer) commandName(path string, list string) (string, bool) {
	if g.lookPath == nil || (list != "rox" && list != "rwx") {
		return "", false
	}
	name := filepath.Base(path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", false
	}
	found, err := g.lookPath(name)
	if err != nil {
		return "", false
	}
	return name, sameResolvedPath(found, path)
}

func sameResolvedPath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	realA, errA := filepath.EvalSymlinks(a)
	realB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && realA == realB
}

// pathUnder reports whether path is root or lies beneath it, returning the
// remainder relative to root. The separator check keeps /home/user-backup
// from matching under /home/user.
func pathUnder(path string, root string) (rest string, ok bool) {
	if path == root {
		return "", true
	}
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", false
	}
	return strings.TrimPrefix(path, root+string(filepath.Separator)), true
}

// mergeEntries collapses entries that name the same thing, keeping the
// weakest access that covers every grant made against it. A file both read
// and executed becomes one rox entry rather than two.
func mergeEntries(entries []recordedEntry) []recordedEntry {
	merged := map[string]*recordedEntry{}
	levels := map[string]access{}
	var order []string
	for _, e := range entries {
		if e.Entry == "" {
			continue
		}
		if _, seen := merged[e.Entry]; !seen {
			copied := e
			merged[e.Entry] = &copied
			order = append(order, e.Entry)
		}
		levels[e.Entry] |= accessForList(e.List)
	}
	out := make([]recordedEntry, 0, len(order))
	for _, key := range order {
		entry := *merged[key]
		entry.List = listForAccess(levels[key])
		out = append(out, entry)
	}
	sortEntries(out)
	return out
}

// finalizeEntries turns the per-denial entries into the set a profile should
// contain: duplicates merged, clusters promoted to directory grants, then
// merged and pruned again.
//
// The second pass is not redundant. Promotion works within one access list at
// a time, so a directory denied both read and write comes out of it twice, and
// a cluster promoted deep can end up inside a directory promoted shallower —
// both of which are entries covered by another entry in the same output.
func finalizeEntries(entries []recordedEntry) []recordedEntry {
	entries = promoteDirectories(mergeEntries(entries))
	return dropCoveredEntries(mergeEntries(entries))
}

// dropCoveredEntries removes entries another directory grant already covers,
// at an access level at least as strong.
//
// Only entries written as directories (a trailing slash, which promotion adds)
// count as covering. A plain path may well be a directory on this machine, but
// assuming so would drop entries on the strength of a guess about a filesystem
// the profile may never run on; leaving a redundant entry is harmless, dropping
// a needed one is not.
func dropCoveredEntries(entries []recordedEntry) []recordedEntry {
	out := make([]recordedEntry, 0, len(entries))
	for _, e := range entries {
		if !coveredByAnother(e, entries) {
			out = append(out, e)
		}
	}
	return out
}

func coveredByAnother(e recordedEntry, entries []recordedEntry) bool {
	for _, other := range entries {
		if other.Entry == e.Entry || !strings.HasSuffix(other.Entry, "/") {
			continue
		}
		if !strings.HasPrefix(e.Entry, other.Entry) {
			continue
		}
		// The covering grant must permit everything this entry asks for.
		if accessForList(e.List)&^accessForList(other.List) == 0 {
			return true
		}
	}
	return false
}

// promotionThreshold is how many sibling entries in one directory justify
// granting the directory instead. Two is a coincidence; three is a pattern,
// and a tool that touched three files in a directory will touch a fourth.
const promotionThreshold = 3

// promoteDirectories replaces clusters of sibling entries with a single grant
// on their parent directory. It runs after generalization, so the safety floor
// is simply that the parent must still have a component of its own below the
// variable or root it sits under — a cluster directly inside $HOME or $CACHE
// promotes to nothing.
func promoteDirectories(entries []recordedEntry) []recordedEntry {
	// Every ancestor of every promotable entry is a candidate, not just the
	// immediate parent. Content-addressed caches fan out over subdirectories
	// holding one file each — a Mesa shader cache writes
	// $CACHE/mesa_shader_cache/0d/<hash> — so grouping by parent alone finds
	// nothing to merge and records a pile of paths that will never recur.
	members := map[string][]int{}
	for i, e := range entries {
		if !promotableEntry(e) {
			continue
		}
		for _, ancestor := range ancestorEntries(e.Entry) {
			key := e.List + "\x00" + ancestor
			members[key] = append(members[key], i)
		}
	}
	candidates := make([]string, 0, len(members))
	for key := range members {
		candidates = append(candidates, key)
	}
	// Deepest first, so the most specific directory that covers a cluster
	// wins and a shallower one never swallows it. Equal depths are ordered by
	// name to keep the result deterministic.
	sort.Slice(candidates, func(i, j int) bool {
		di, dj := strings.Count(candidates[i], "/"), strings.Count(candidates[j], "/")
		if di != dj {
			return di > dj
		}
		return candidates[i] < candidates[j]
	})

	dropped := map[int]bool{}
	var promoted []recordedEntry
	for _, key := range candidates {
		remaining := 0
		for _, i := range members[key] {
			if !dropped[i] {
				remaining++
			}
		}
		if remaining < promotionThreshold {
			continue
		}
		list, dir, _ := strings.Cut(key, "\x00")
		for _, i := range members[key] {
			dropped[i] = true
		}
		promoted = append(promoted, recordedEntry{
			List:    list,
			Entry:   dir + "/",
			Comment: "directory Grant for " + strconv.Itoa(remaining) + " files denied inside it",
		})
	}
	out := make([]recordedEntry, 0, len(entries))
	for i, e := range entries {
		if !dropped[i] {
			out = append(out, e)
		}
	}
	out = append(out, promoted...)
	sortEntries(out)
	return out
}

// promotableEntry reports whether an entry is a plain path that could be
// replaced by a grant on its parent. Resolver entries already name a whole
// directory, and an entry that is already a directory grant is not promoted
// further: growing "$CACHE/x/" into "$CACHE/" is exactly the widening the
// floor exists to prevent.
func promotableEntry(e recordedEntry) bool {
	if e.Entry == "" || strings.HasSuffix(e.Entry, "/") || bpaths.IsResolverEntry(e.Entry) {
		return false
	}
	// A package root's parent is the store itself. Merging several of them
	// would replace one grant per package with a grant on every package on the
	// machine, undoing the collapse that produced them.
	if isPackageStoreRoot(e.Entry) {
		return false
	}
	return len(ancestorEntries(e.Entry)) > 0
}

// ancestorEntries lists the directories an entry could be merged into,
// deepest first, keeping only those that clear the safety floor.
func ancestorEntries(entry string) []string {
	var out []string
	for dir := parentEntry(entry); dir != ""; dir = parentEntry(dir) {
		if isStoreRootDirectory(dir) {
			// Nothing above a package store root is safe either.
			break
		}
		if promotableDirectory(dir) {
			out = append(out, dir)
		}
	}
	return out
}

// promotableDirectory reports whether a directory is deep enough to grant.
// It must have a component of its own below whatever root it hangs from:
// "$CACHE/foo" qualifies and "$CACHE" does not; "/usr/lib/x" qualifies and
// "/usr" does not. Those roots are the trees a sandbox exists to withhold.
func promotableDirectory(dir string) bool {
	if dir == "" || isStoreRootDirectory(dir) {
		return false
	}
	var rest string
	if strings.HasPrefix(dir, "$") {
		_, rest, _ = strings.Cut(dir, "/")
	} else {
		rest = strings.TrimPrefix(dir, "/")
		if i := strings.Index(rest, "/"); i >= 0 {
			rest = rest[i+1:]
		} else {
			rest = ""
		}
	}
	return rest != ""
}

// isStoreRootDirectory reports whether a path is the root of a package store
// (/nix/store, /opt/homebrew/Cellar). Promoting into one is never right: it
// grants every package installed on the machine, whatever the entries below it
// happened to be.
func isStoreRootDirectory(path string) bool {
	clean := strings.TrimSuffix(path, "/")
	for _, root := range storeGrantRoots {
		if clean == strings.TrimSuffix(root.prefix, "/") {
			return true
		}
	}
	return false
}

func parentEntry(entry string) string {
	i := strings.LastIndex(entry, "/")
	if i <= 0 {
		return ""
	}
	return entry[:i]
}

func sortEntries(entries []recordedEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].List != entries[j].List {
			return entries[i].List < entries[j].List
		}
		return entries[i].Entry < entries[j].Entry
	})
}
