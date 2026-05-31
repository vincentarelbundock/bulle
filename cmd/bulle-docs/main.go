package main

import (
	"fmt"
	"os"

	"github.com/vincentarelbundock/bulle/internal/cli"
)

func main() {
	out := cli.ReferenceMarkdown()
	if err := os.WriteFile("docs-src/cli-reference.md", []byte(out), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write CLI reference: %v\n", err)
		os.Exit(1)
	}
}
