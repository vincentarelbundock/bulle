package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/vincentarelbundock/bulle/internal/backends"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

const preparedPolicyRunnerCommand = "__run-prepared-policy"

func isPreparedPolicyRunner(args []string) bool {
	return len(args) > 1 && args[1] == preparedPolicyRunnerCommand
}

func runPreparedPolicy(args []string, stderr io.Writer) int {
	if len(args) != 4 || args[2] != "--policy-fd" {
		fmt.Fprintln(stderr, "usage: bulle __run-prepared-policy --policy-fd FD")
		return ExitSandboxSetup
	}
	fd, err := strconv.Atoi(args[3])
	if err != nil || fd < 0 {
		fmt.Fprintf(stderr, "invalid policy fd %q\n", args[3])
		return ExitSandboxSetup
	}
	file := os.NewFile(uintptr(fd), "prepared-policy")
	if file == nil {
		fmt.Fprintf(stderr, "invalid policy fd %q\n", args[3])
		return ExitSandboxSetup
	}

	var p policy.Policy
	if err := json.NewDecoder(file).Decode(&p); err != nil {
		_ = file.Close()
		fmt.Fprintf(stderr, "decode prepared policy: %v\n", err)
		return ExitSandboxSetup
	}
	if err := file.Close(); err != nil {
		fmt.Fprintf(stderr, "close prepared policy fd: %v\n", err)
		return ExitSandboxSetup
	}
	return runPreparedPolicyBackend(p, stderr)
}

var runPreparedPolicyBackend = func(p policy.Policy, stderr io.Writer) int {
	backend, err := backends.ForName(p.Backend)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitBackendMissing
	}
	if err := backend.Run(p); err != nil {
		fmt.Fprintln(stderr, err)
		if isCommandExitError(err) {
			return ExitCommandFailed
		}
		return ExitSandboxSetup
	}
	return ExitOK
}
