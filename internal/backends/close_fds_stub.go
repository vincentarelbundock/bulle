//go:build !linux && !darwin

package backends

func closeUnexpectedFileDescriptors() error { return nil }
