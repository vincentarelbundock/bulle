package env

import (
	"os"
	"strings"
)

// Parent snapshots the calling process's environment as a map, the shape
// Resolve and policy resolution both take a parent environment in.
func Parent() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}
