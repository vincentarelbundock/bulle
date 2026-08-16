package integration

import (
	"os"
	"testing"
)

func linuxROPathArgs(paths ...string) []string {
	args := []string{}
	for _, path := range paths {
		args = append(args, "--ro", path)
	}
	return args
}

func linuxROXPathArgs(paths ...string) []string {
	args := []string{}
	for _, path := range paths {
		args = append(args, "--rox", path)
	}
	return args
}

func linuxRuntimePathArgs(extra ...string) []string {
	args := linuxROPathArgs("/dev/null")
	return append(args, linuxROXPathArgs(append([]string{"/bin", "/usr/bin", "/lib", "/lib64", "/usr/lib", "/usr/lib64"}, extra...)...)...)
}

func TestLinuxRuntimePathArgsAllowShellBackgroundStdinDevice(t *testing.T) {
	args := linuxRuntimePathArgs()
	if !argPairContains(args, "--ro", "/dev/null") {
		t.Fatalf("linuxRuntimePathArgs() = %#v, want read-only /dev/null for shell background jobs", args)
	}
}

func argPairContains(args []string, flag string, value string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// requireExecutables skips a test whose fixture is a well-known system binary
// the machine does not have. /usr/bin/true and /bin/sleep exist on a
// distribution runner and not on NixOS, where every binary lives in the store;
// a test that fails there is reporting the layout of the machine, not a defect.
func requireExecutables(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Skipf("%s is not an executable on this machine", path)
		}
	}
}
