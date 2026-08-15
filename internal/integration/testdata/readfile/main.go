// Command readfile reads the file named by its first argument and exits
// non-zero if it cannot. It exists so the record-mode integration test has a
// command whose only interesting behavior is one filesystem access.
//
// It is built with CGO_ENABLED=0 so the resulting binary is static: granting
// it execute access is then the whole story, with no dynamic loader or shared
// libraries to discover. That matters because library discovery fails on
// distributions without an ld.so cache, and a recording test that depended on
// it would be testing the wrong thing.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: readfile PATH")
		os.Exit(2)
	}
	if _, err := os.ReadFile(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "readfile: %v\n", err)
		os.Exit(1)
	}
	fmt.Print("read-ok")
}
