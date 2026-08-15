package cli

import (
	"strings"
	"testing"
)

func TestManPageCoversTheWholeCLI(t *testing.T) {
	page := ManPage("1.2.3")
	if !strings.HasPrefix(page, `.TH BULLE 1 "" "bulle 1.2.3"`) {
		t.Fatalf("man page missing TH header:\n%s", page[:80])
	}
	// Every visible subcommand and every help topic must appear, so the man
	// page cannot silently drop a section as the CLI evolves.
	for name := range subcommandHelp {
		if !strings.Contains(page, ".SS "+name) {
			t.Errorf("man page missing subcommand section %q", name)
		}
	}
	for _, heading := range []string{"GRANTS", "ENVIRONMENT", "LIMITS", "CONFIGURATION"} {
		if !strings.Contains(page, ".SH "+heading) {
			t.Errorf("man page missing topic section %q", heading)
		}
	}
	for _, want := range []string{
		"run coding agents and other dangerous tools inside a sandbox",
		`\-\-ro PATH`,
		"which:NAME",
		`\-\-timeout DURATION`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("man page missing %q", want)
		}
	}
}

func TestManEscape(t *testing.T) {
	if got := manEscape(`.\-x`); got != `\&.\\\-x` {
		t.Fatalf("manEscape = %q", got)
	}
	if got := manEscape("plain text"); got != "plain text" {
		t.Fatalf("manEscape mangled plain text: %q", got)
	}
}
