package app

import (
	"os"
	"testing"
)

// TestMain routes the internal runner invocations back through Run. A test
// that really runs a sandboxed command makes the supervisor re-exec this test
// binary as `app.test __run-prepared-policy ...`; without this interception
// that child would run the whole test suite again, recursively.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (isPreparedPolicyRunner(os.Args) || isDenialLoggingProbe(os.Args) || os.Args[1] == preparedPolicyRunnerCommand) {
		os.Exit(Run(os.Args, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}
