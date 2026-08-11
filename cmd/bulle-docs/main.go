package main

import (
	"fmt"
	"os"

	"github.com/vincentarelbundock/bulle/internal/cli"
)

func main() {
	out := cli.ReferenceTypst()
	if err := os.WriteFile("docs-src/cli-reference.typ", []byte(out), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write CLI reference: %v\n", err)
		os.Exit(1)
	}
}
