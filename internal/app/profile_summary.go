package app

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func shouldPrintProfileSummary(opts cli.Options) bool {
	return opts.Profile != "" && !opts.Policy
}

func writeProfilePermissionSummary(profileName string, p policy.Policy, w io.Writer) {
	view := policy.NewView(p)
	paths := newPathSummaryFormatter(p)
	fmt.Fprintf(w, "bulle profile %q permissions:\n", profileName)
	fmt.Fprintf(w, "  backend: %s\n", view.Backend)
	fmt.Fprintf(w, "  command: %s\n", formatCommand(view.Command))
	fmt.Fprintf(w, "  workspace: %s\n", paths.formatProject(view.ProjectPath))
	fmt.Fprintln(w, "  filesystem:")
	writePermissionGroup(w, "ro", view.ReadOnly, paths)
	writePermissionGroup(w, "rox", view.ReadOnlyExec, paths)
	writePermissionGroup(w, "rw", view.ReadWrite, paths)
	writePermissionGroup(w, "rwx", view.ReadWriteExec, paths)
	fmt.Fprintf(w, "  environment: %s\n", formatInlineList(view.EnvKeys))
	fmt.Fprintf(w, "  network: %s\n", formatNetwork(view.Network))
	fmt.Fprintf(w, "  add_exec: %s\n", formatEnabled(view.AddExec))
	fmt.Fprintf(w, "  add_libs: %s\n", formatEnabled(view.AddLibs))
	fmt.Fprintf(w, "  keychain: %s\n", formatEnabled(view.AllowKeychain))
}

func preRunSessionPaste(opts cli.Options, p policy.Policy) string {
	if !shouldPrintProfileSummary(opts) {
		return ""
	}
	var b strings.Builder
	writeProfilePermissionSummary(opts.Profile, p, &b)
	return "For context, bulle launched this session with the following sandbox permissions. Use this as background information; no response is required.\n\n" + b.String()
}

func commandWithSessionPermissions(profileName string, command []string, summary string) []string {
	if summary == "" || len(command) == 0 || filepath.Base(command[0]) != profileName {
		return command
	}
	out := make([]string, 0, len(command)+2)
	out = append(out, command[0])
	switch profileName {
	case "claude":
		out = append(out, "--system-prompt", summary)
	case "pi":
		out = append(out, "--append-system-prompt", summary)
	case "opencode":
		out = append(out, "--prompt", summary)
	case "codex":
		out = append(out, command[1:]...)
		return append(out, summary)
	default:
		return command
	}
	return append(out, command[1:]...)
}

func writePermissionGroup(w io.Writer, label string, values []string, paths pathSummaryFormatter) {
	if len(values) == 0 {
		fmt.Fprintf(w, "    %s: none\n", label)
		return
	}
	writeWrappedItems(w, fmt.Sprintf("    %s: ", label), "        ", paths.formatList(values))
}

type pathSummaryFormatter struct {
	project string
	home    string
	tmp     string
}

func newPathSummaryFormatter(p policy.Policy) pathSummaryFormatter {
	return pathSummaryFormatter{
		project: cleanPath(p.ProjectPath),
		home:    cleanPath(p.Env["HOME"]),
		tmp:     cleanPath(firstSet(p.Env["TMP"], p.Env["TMPDIR"], p.Env["TEMP"], p.Env["BUN_TMPDIR"])),
	}
}

func (f pathSummaryFormatter) formatProject(path string) string {
	return f.formatPath(path, false)
}

func (f pathSummaryFormatter) formatList(values []string) []string {
	collapsed := f.collapsePrivateAliases(values)
	grouped := groupSiblingPaths(collapsed)
	if len(grouped) == 0 {
		return []string{"none"}
	}
	return grouped
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
	return clean
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
