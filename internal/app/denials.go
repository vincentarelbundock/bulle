package app

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A landlockDenial is one Landlock audit record emitted by the kernel for an
// access the sandbox refused (Linux 6.15+, Landlock ABI v7).
type landlockDenial struct {
	Blockers []string
	Path     string
	Raw      string
}

// Example kernel audit records, as they appear in journalctl -k / dmesg:
//
//	audit: type=1423 audit(1729738800.268:30): domain=195ba459b blockers=fs.read_file path="/etc/shadow" dev="vda2" ino=1523541
//	audit: type=1423 audit(1729738800.268:31): domain=195ba459b blockers=net.connect_tcp daddr=127.0.0.1 dest=443
var landlockDenialPattern = regexp.MustCompile(`\bblockers=([a-z_.,]+)(?:\s+path="((?:[^"\\]|\\.)*)")?`)

var kernelTimestampPattern = regexp.MustCompile(`^\[\s*(\d+\.\d+)\]`)

// parseLandlockDenials extracts Landlock denial records from kernel log lines.
// Lines with a leading "[ seconds.frac]" boot-relative timestamp (dmesg style)
// are skipped when older than sinceUptime seconds; pass 0 to keep everything.
func parseLandlockDenials(lines []string, sinceUptime float64) []landlockDenial {
	var denials []landlockDenial
	for _, line := range lines {
		if ts := kernelTimestampPattern.FindStringSubmatch(line); ts != nil && sinceUptime > 0 {
			if at, err := strconv.ParseFloat(ts[1], 64); err == nil && at < sinceUptime {
				continue
			}
		}
		match := landlockDenialPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		denials = append(denials, landlockDenial{
			Blockers: strings.Split(match[1], ","),
			Path:     unescapeAuditPath(match[2]),
			Raw:      strings.TrimSpace(line),
		})
	}
	return denials
}

func unescapeAuditPath(path string) string {
	if !strings.Contains(path, `\`) {
		return path
	}
	replacer := strings.NewReplacer(`\"`, `"`, `\\`, `\`)
	return replacer.Replace(path)
}

// denialHints turns denial records into deduplicated, copy-pasteable
// suggestion lines like:
//
//	denied: read /home/user/.gitconfig — add --ro ~/.gitconfig
func denialHints(denials []landlockDenial, home string) []string {
	seen := map[string]bool{}
	var hints []string
	for _, d := range denials {
		hint := hintForDenial(d, home)
		if hint == "" || seen[hint] {
			continue
		}
		seen[hint] = true
		hints = append(hints, hint)
	}
	sort.Strings(hints)
	return hints
}

func hintForDenial(d landlockDenial, home string) string {
	verb, flag := classifyBlockers(d.Blockers)
	if verb == "" {
		return ""
	}
	if flag == "" {
		// Network denial: no path flag to suggest, but the attempt is worth
		// surfacing with whatever detail the record carries (daddr, dest, ...).
		detail := ""
		if i := strings.Index(d.Raw, "blockers="); i >= 0 {
			detail = " (" + d.Raw[i:] + ")"
		}
		return fmt.Sprintf("denied: %s — network access is restricted by the sandbox policy%s", verb, detail)
	}
	g, ok := grantForDenial(d)
	if !ok {
		return ""
	}
	if g.Path != d.Path {
		// Naming the package root in both halves lets hint deduplication
		// collapse every denial inside the same store item into one line, so
		// the 10-hint display cap is spent on distinct grants.
		return fmt.Sprintf("denied: %s files in %s — add %s %s", verb, g.Path, g.Flag, abbreviateHome(g.Path, home))
	}
	return fmt.Sprintf("denied: %s %s — add %s %s", verb, d.Path, g.Flag, abbreviateHome(d.Path, home))
}

// A grant is the policy entry that would allow a denied access: the flag
// naming the access level, and the path to grant. The path is the one worth
// granting rather than the one denied — inside a package store it is the
// package root — but it is otherwise verbatim, with no $HOME abbreviation and
// no variable substitution, both of which are presentation concerns.
//
// This is the machine-readable half of a denial hint. hintForDenial formats it
// for a human to copy; profile authoring consumes it directly.
type grant struct {
	Flag string
	Path string
}

// grantForDenial maps a Landlock denial record to the grant that would allow
// it. It reports false for records that name no grantable path: network
// denials, which the policy governs as a whole rather than per-address, and
// records the kernel emitted without a path.
func grantForDenial(d landlockDenial) (grant, bool) {
	_, flag := classifyBlockers(d.Blockers)
	if flag == "" || d.Path == "" {
		return grant{}, false
	}
	return grant{Flag: flag, Path: grantSuggestionPath(d.Path)}, true
}

// storeGrantRoots are per-package installer trees where the natural grant is
// the package's own root, not the individual denied file. Collapsing to the
// root turns dozens of denials inside one Nix store item or Homebrew keg into
// a single suggestion, so following the hints converges in one retry instead
// of one file at a time.
var storeGrantRoots = []struct {
	prefix string
	// components is the path depth of the package root, counting from "/":
	// /nix/store/<item> is 3, /opt/homebrew/Cellar/<name>/<version> is 5.
	components int
}{
	{"/nix/store/", 3},
	{"/gnu/store/", 3},
	{"/opt/homebrew/Cellar/", 5},
	{"/usr/local/Cellar/", 5},
}

// grantSuggestionPath maps a denied path to the path a hint should suggest
// granting: the enclosing package root inside a package store, the denied
// path itself everywhere else.
func grantSuggestionPath(path string) string {
	if procGrantPath(path) {
		return "/proc"
	}
	for _, root := range storeGrantRoots {
		if !strings.HasPrefix(path, root.prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) <= root.components {
			return path
		}
		return "/" + strings.Join(parts[:root.components], "/")
	}
	return path
}

// procGrantPath reports whether a denied path is a per-process /proc entry
// such as /proc/1234/cgroup.
//
// The kernel reports these with the pid resolved, and every process has a
// different one, so the denied path is never the path to grant: it names a
// process that has already exited. /proc/self does not cover it either, since
// that resolves at grant time to the granting process rather than to the child
// that hit the denial — which is why the quarto profile grants /proc whole and
// documents the tradeoff.
//
// Collapsing to /proc is what lets a suggestion be followed once instead of
// once per process, and what lets a recording converge at all: without it,
// every round observes a new pid and looks like new information forever.
func procGrantPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/proc/")
	if !ok {
		return false
	}
	pid, _, _ := strings.Cut(rest, "/")
	if pid == "" {
		return false
	}
	for _, r := range pid {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// classifyBlockers maps a record's blockers to a human verb and the grant flag
// that would allow the strongest denied access (execute > write > read).
func classifyBlockers(blockers []string) (verb string, flag string) {
	var read, write, exec, net bool
	for _, b := range blockers {
		switch {
		case b == "fs.execute":
			exec = true
		case b == "fs.read_file" || b == "fs.read_dir":
			read = true
		case strings.HasPrefix(b, "fs."):
			write = true
		case strings.HasPrefix(b, "net."):
			net = true
			verb = strings.TrimPrefix(b, "net.")
		}
	}
	switch {
	case exec && write:
		return "execute+write", "--rwx"
	case exec:
		return "execute", "--rox"
	case write:
		return "write", "--rw"
	case read:
		return "read", "--ro"
	case net:
		return strings.ReplaceAll(verb, "_", " "), ""
	}
	return "", ""
}

// A seatbeltDenial is one sandbox violation reported by the macOS kernel in
// the unified log.
type seatbeltDenial struct {
	Process   string
	Operation string
	Path      string
}

// Example unified log lines (log show --style syslog):
//
//	2026-08-10 12:00:00.123456-0400  localhost kernel[0]: (Sandbox) Sandbox: cat(1234) deny(1) file-read-data /Users/v/.gitconfig
//	2026-08-10 12:00:01.000000-0400  localhost kernel[0]: (Sandbox) Sandbox: curl(1235) deny(1) network-outbound *:443
var seatbeltDenialPattern = regexp.MustCompile(`Sandbox: (\S+)\((\d+)\) deny\(\d+\) ([a-z][a-z0-9.*-]*)(?: (.+))?$`)

// parseSeatbeltDenials extracts sandbox violations from unified log lines.
// Only file, exec, and network operations are kept: Seatbelt profiles deny
// many benign mach-lookup/sysctl probes that would drown real hints.
func parseSeatbeltDenials(lines []string) []seatbeltDenial {
	var denials []seatbeltDenial
	for _, line := range lines {
		match := seatbeltDenialPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		op := match[3]
		if !strings.HasPrefix(op, "file-") && !strings.HasPrefix(op, "process-exec") && !strings.HasPrefix(op, "network-") {
			continue
		}
		denials = append(denials, seatbeltDenial{
			Process:   match[1],
			Operation: op,
			Path:      strings.TrimSpace(match[4]),
		})
	}
	return denials
}

// seatbeltHints turns Seatbelt violations into deduplicated, copy-pasteable
// suggestion lines, mirroring the Landlock hint format.
func seatbeltHints(denials []seatbeltDenial, home string) []string {
	seen := map[string]bool{}
	var hints []string
	for _, d := range denials {
		hint := hintForSeatbeltDenial(d, home)
		if hint == "" || seen[hint] {
			continue
		}
		seen[hint] = true
		hints = append(hints, hint)
	}
	sort.Strings(hints)
	return hints
}

func hintForSeatbeltDenial(d seatbeltDenial, home string) string {
	if strings.HasPrefix(d.Operation, "network-") {
		target := ""
		if d.Path != "" {
			target = " to " + d.Path
		}
		return fmt.Sprintf("denied: %s%s (%s) — network access is restricted by the sandbox policy",
			strings.ReplaceAll(strings.TrimPrefix(d.Operation, "network-"), "-", " "), target, d.Process)
	}
	verb, _ := classifySeatbeltOperation(d.Operation)
	g, ok := grantForSeatbeltDenial(d)
	if !ok {
		return ""
	}
	if g.Path != d.Path {
		return fmt.Sprintf("denied: %s files in %s (%s) — add %s %s", verb, g.Path, d.Process, g.Flag, abbreviateHome(g.Path, home))
	}
	return fmt.Sprintf("denied: %s %s (%s) — add %s %s", verb, d.Path, d.Process, g.Flag, abbreviateHome(d.Path, home))
}

// classifySeatbeltOperation maps a sandbox operation name to a human verb and
// the grant flag that would allow it, mirroring classifyBlockers on Linux.
func classifySeatbeltOperation(operation string) (verb string, flag string) {
	switch {
	case strings.HasPrefix(operation, "process-exec"):
		return "execute", "--rox"
	case strings.HasPrefix(operation, "file-write"):
		return "write", "--rw"
	case strings.HasPrefix(operation, "file-read"):
		return "read", "--ro"
	}
	return "", ""
}

// grantForSeatbeltDenial maps a Seatbelt violation to the grant that would
// allow it. It reports false for violations that name no grantable path:
// network operations, paths the log redacted to <private>, and non-file
// operations whose target is not a path at all.
func grantForSeatbeltDenial(d seatbeltDenial) (grant, bool) {
	if d.Path == "" || d.Path == "<private>" || !strings.HasPrefix(d.Path, "/") {
		return grant{}, false
	}
	_, flag := classifySeatbeltOperation(d.Operation)
	if flag == "" {
		return grant{}, false
	}
	return grant{Flag: flag, Path: grantSuggestionPath(d.Path)}, true
}

func abbreviateHome(path string, home string) string {
	if home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}
