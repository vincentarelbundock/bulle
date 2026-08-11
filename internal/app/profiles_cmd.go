package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
)

// runProfilesCommand implements `bulle profiles list|install`, honoring
// --config the way runs do.
func runProfilesCommand(args []string, stdout, stderr io.Writer) int {
	args, configRoot, err := extractConfigFlag(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitConfigError
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: bulle profiles list | bulle profiles install SOURCE")
		return ExitConfigError
	}
	switch args[0] {
	case "list":
		if len(args) > 1 {
			fmt.Fprintln(stderr, "usage: bulle profiles list")
			return ExitConfigError
		}
		global, err := loadConfig(cli.Options{Flags: cli.Flags{Config: configRoot}})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ExitConfigError
		}
		for _, name := range cli.ProfileNames(global) {
			fmt.Fprintln(stdout, name)
		}
		return ExitOK
	case "install":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: bulle profiles install SOURCE")
			return ExitConfigError
		}
		root := configRoot
		if root == "" {
			root = defaultConfigRoot()
		}
		if root == "" {
			fmt.Fprintln(stderr, "could not determine user config directory")
			return ExitConfigError
		}
		if err := installProfiles(args[1], root, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return ExitConfigError
		}
		return ExitOK
	default:
		fmt.Fprintf(stderr, "bulle: unknown profiles command %q; use list or install\n", args[0])
		return ExitConfigError
	}
}

func extractConfigFlag(args []string) ([]string, string, error) {
	out := make([]string, 0, len(args))
	config := ""
	for i := 0; i < len(args); i++ {
		if value, ok := strings.CutPrefix(args[i], "--config="); ok {
			config = value
			continue
		}
		if args[i] == "--config" {
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--config requires a path")
			}
			config = args[i+1]
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out, config, nil
}
