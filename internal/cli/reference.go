package cli

import (
	"fmt"
	"strings"
)

// ReferenceTypst returns a Typst CLI reference page for the Calepin website,
// generated from the same full help text printed by bulle --help and the help
// subcommand. The help text is emitted inside a raw block, so it needs no
// escaping; the fence is four backticks so a help text containing a triple
// backtick could not close it early.
func ReferenceTypst() string {
	body := strings.TrimRight(Usage(), "\n")
	return fmt.Sprintf("#set document(title: [CLI Reference])\n"+
		"#metadata((\n"+
		"  title: \"CLI Reference\",\n"+
		"  description: \"Command-line reference for bulle.\",\n"+
		")) <website-metadata>\n"+
		"\n"+
		"#title()\n"+
		"\n"+
		"This page is generated from bulle --help.\n"+
		"\n"+
		"````text\n"+
		"%s\n"+
		"````\n", body)
}
