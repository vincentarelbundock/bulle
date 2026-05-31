package cli

import (
	"fmt"
	"strings"
)

// ReferenceMarkdown returns a Markdown CLI reference generated from the same
// full help text printed by bulle --help and the help subcommand.
func ReferenceMarkdown() string {
	body := strings.TrimRight(Usage(), "\n")
	return fmt.Sprintf(`---
title: CLI reference
description: Command-line reference for bulle.
hide:
  - navigation
---

# CLI reference

This page is generated from bulle --help.

~~~text
%s
~~~
`, body)
}
