package main

import (
	"os"

	"github.com/vincentarelbundock/bulle/internal/app"
)

func main() {
	code := app.Run(os.Args, os.Stdout, os.Stderr)
	os.Exit(code)
}
