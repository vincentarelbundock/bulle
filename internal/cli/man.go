package cli

import (
	"fmt"
	"strings"
)

// ManPage renders the bulle.1 man page as roff, assembled from the same
// strings the terminal help prints — the front usage screen, the subcommand
// help, and the help topics — so the man page cannot drift from the CLI.
func ManPage(version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, ".TH BULLE 1 \"\" \"bulle %s\" \"User Commands\"\n", version)
	b.WriteString(".SH NAME\n")
	b.WriteString("bulle \\- run coding agents and other dangerous tools inside a sandbox\n")
	b.WriteString(".SH SYNOPSIS\n")
	b.WriteString(".nf\n")
	b.WriteString(manEscape("bulle <profile>[,profile...] [dir] [-- command [args...]]") + "\n")
	b.WriteString(".fi\n")

	b.WriteString(".SH DESCRIPTION\n")
	// The front help screen minus its first line, which NAME already covers.
	_, body, _ := strings.Cut(usageText, "\n")
	writeManBody(&b, body)

	b.WriteString(".SH SUBCOMMANDS\n")
	for _, name := range []string{"show", "scratch", "profiles", "completion", "help", "version"} {
		fmt.Fprintf(&b, ".SS %s\n", name)
		writeManBody(&b, subcommandHelp[name])
	}

	for _, topic := range []struct{ name, heading string }{
		{"grants", "GRANTS"},
		{"env", "ENVIRONMENT"},
		{"limits", "LIMITS"},
		{"config", "CONFIGURATION"},
	} {
		fmt.Fprintf(&b, ".SH %s\n", topic.heading)
		writeManBody(&b, helpTopics[topic.name])
	}

	b.WriteString(".SH SEE ALSO\n")
	b.WriteString("Full documentation at " + manEscape("https://vincentarelbundock.github.io/bulle") + "\n")
	return b.String()
}

// writeManBody converts plain help text to roff: indented lines are kept
// verbatim in no-fill blocks, everything else flows as filled paragraphs.
func writeManBody(b *strings.Builder, text string) {
	inBlock := false
	paragraphOpen := false
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		indented := strings.HasPrefix(line, "  ")
		switch {
		case line == "":
			if inBlock {
				b.WriteString(".fi\n")
				inBlock = false
			}
			paragraphOpen = false
		case indented && !inBlock:
			b.WriteString(".nf\n")
			inBlock = true
			fallthrough
		default:
			if !indented && inBlock {
				b.WriteString(".fi\n")
				inBlock = false
			}
			if !indented && !paragraphOpen {
				b.WriteString(".PP\n")
				paragraphOpen = true
			}
			b.WriteString(manEscape(line) + "\n")
		}
	}
	if inBlock {
		b.WriteString(".fi\n")
	}
}

// manEscape makes a text line safe for roff: backslashes are doubled, bare
// hyphens become explicit minus signs, and a leading dot or quote is
// neutralized so the line cannot be read as a request.
func manEscape(line string) string {
	line = strings.ReplaceAll(line, `\`, `\\`)
	line = strings.ReplaceAll(line, "-", `\-`)
	if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
		line = `\&` + line
	}
	return line
}
