package integration

import (
	"os/exec"
	"testing"
)

func buildBulleForIntegration(t *testing.T, bin string) {
	t.Helper()
	out, err := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "../../cmd/bulle").CombinedOutput()
	if err != nil {
		t.Fatalf("build bulle: %v, output: %s", err, string(out))
	}
}
