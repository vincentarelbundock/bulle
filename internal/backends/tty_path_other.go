//go:build !darwin

package backends

// fcntlGetPath has no effect off macOS; the seatbelt profile that consumes it
// is only built on darwin.
func fcntlGetPath(int) (path string, ok bool) { return "", false }
