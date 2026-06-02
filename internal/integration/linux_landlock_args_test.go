package integration

import "testing"

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
