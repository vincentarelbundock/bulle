package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/vincentarelbundock/bulle/internal/backends"
	"github.com/vincentarelbundock/bulle/internal/exitcode"
	"github.com/vincentarelbundock/bulle/internal/limits"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

const preparedPolicyRunnerCommand = "__run-prepared-policy"

func isPreparedPolicyRunner(args []string) bool {
	return len(args) == 4 && args[1] == preparedPolicyRunnerCommand && args[2] == "--policy-fd"
}

func runPreparedPolicy(args []string, stderr io.Writer) int {
	if len(args) != 4 || args[2] != "--policy-fd" {
		fmt.Fprintln(stderr, "usage: bulle __run-prepared-policy --policy-fd FD")
		return exitcode.SandboxSetup
	}
	fd, err := strconv.Atoi(args[3])
	if err != nil || fd < 0 {
		fmt.Fprintf(stderr, "invalid policy fd %q\n", args[3])
		return exitcode.SandboxSetup
	}
	file := os.NewFile(uintptr(fd), "prepared-policy")
	if file == nil {
		fmt.Fprintf(stderr, "invalid policy fd %q\n", args[3])
		return exitcode.SandboxSetup
	}

	var p policy.Policy
	if err := json.NewDecoder(file).Decode(&p); err != nil {
		_ = file.Close()
		fmt.Fprintf(stderr, "decode prepared policy: %v\n", err)
		return exitcode.SandboxSetup
	}
	if err := file.Close(); err != nil {
		fmt.Fprintf(stderr, "close prepared policy fd: %v\n", err)
		return exitcode.SandboxSetup
	}
	return runPreparedPolicyBackend(p, stderr)
}

var runPreparedPolicyBackend = func(p policy.Policy, stderr io.Writer) int {
	backend, err := backends.ForName(p.Backend)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.BackendMissing
	}
	// Set the portable limits here, in the sandboxed child: applying them in
	// the supervisor would constrain bulle itself, and the Linux backend execs
	// into the target, so this is the last point that is still shared by both
	// platforms. rlimits survive the exec.
	if err := limits.ApplyRlimits(p.Limits); err != nil {
		fmt.Fprintln(stderr, err)
		return exitcode.SandboxSetup
	}
	if err := backend.Run(p); err != nil {
		fmt.Fprintln(stderr, err)
		if isCommandExitError(err) {
			return exitcode.CommandFailed
		}
		return exitcode.SandboxSetup
	}
	return exitcode.OK
}
