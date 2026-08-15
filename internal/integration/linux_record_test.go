//go:build linux

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildStaticHelper compiles the readfile helper as a static binary, so the
// sandbox only has to grant execute on the binary itself.
func buildStaticHelper(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./testdata/readfile")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build readfile helper: %v, output: %s", err, string(out))
	}
}

// runRecord invokes record mode and skips the test when this machine cannot
// record. Recording needs Landlock audit logging that actually reaches the
// kernel log, which is a property of the kernel and its boot parameters, not
// of the code under test.
func runRecord(t *testing.T, bin string, args ...string) (stdout string, stderr string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"record"}, args...)...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	stdout, stderr = outBuf.String(), errBuf.String()
	if strings.Contains(stderr, "recording needs") || strings.Contains(stderr, "never reached the log") {
		t.Skipf("recording unsupported here: %s", strings.TrimSpace(stderr))
	}
	if err != nil {
		t.Fatalf("record failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	return stdout, stderr
}

// TestLinuxRecordConvergesOnADeniedRead is the end-to-end check of the
// recording loop: a command denied one file must be granted that file and
// succeed on a later round, and the emitted profile must contain the grant.
func TestLinuxRecordConvergesOnADeniedRead(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bulle")
	buildBulleForIntegration(t, bin)
	helper := filepath.Join(dir, "readfile")
	buildStaticHelper(t, helper)

	project := t.TempDir()
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "wanted.txt")
	if err := os.WriteFile(secret, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runRecord(t, bin,
		"--profile", "default", "--max-rounds", "6", "--",
		project, "--rox", helper, "--", helper, secret,
	)

	if !strings.Contains(stderr, "command succeeded") {
		t.Fatalf("recording did not converge:\nstderr:\n%s\nprofile:\n%s", stderr, stdout)
	}
	// More than one round is the point: round one is denied, and a later round
	// runs with the grant the denial produced.
	if !strings.Contains(stderr, "round 2") {
		t.Errorf("expected a second round after the denial:\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stdout, secret) && !strings.Contains(stdout, secretDir) {
		t.Errorf("profile does not grant the denied file %q:\n%s", secret, stdout)
	}
	if !strings.Contains(stdout, `inherits = ["default"]`) {
		t.Errorf("profile does not inherit from the base:\n%s", stdout)
	}
	// Every recorded entry is optional, so a profile still resolves on a
	// machine that lacks one of these paths.
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, `"`) {
			continue
		}
		if !strings.HasPrefix(trimmed, `"?`) {
			t.Errorf("recorded entry is not optional: %s", trimmed)
		}
	}
}

// TestLinuxRecordStopsWhenNoGrantWouldHelp covers the other stop condition: a
// command that fails for a reason no grant fixes must not be retried to the
// round cap, and the profile must say the entries are not sufficient.
func TestLinuxRecordStopsWhenNoGrantWouldHelp(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bulle")
	buildBulleForIntegration(t, bin)
	helper := filepath.Join(dir, "readfile")
	buildStaticHelper(t, helper)

	project := t.TempDir()
	// The file does not exist, so no grant can make the read succeed.
	missing := filepath.Join(project, "absent.txt")

	stdout, stderr := runRecord(t, bin,
		"--profile", "default", "--max-rounds", "6", "--",
		project, "--rox", helper, "--", helper, missing,
	)

	if !strings.Contains(stderr, "no grant will fix that") {
		t.Fatalf("recording did not stop on a non-denial failure:\nstderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "round 6") {
		t.Errorf("recording ran to the cap instead of stopping early:\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stdout, "not sufficient") {
		t.Errorf("profile does not warn that the entries are insufficient:\n%s", stdout)
	}
}

// TestLinuxRecordRefusesWithoutABaseProfile keeps the argument contract
// covered end to end: recording always diffs against something.
func TestLinuxRecordRefusesWithoutABaseProfile(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)

	cmd := exec.Command(bin, "record", "--", "true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("record without --profile succeeded: %s", string(out))
	}
	if !strings.Contains(string(out), "needs a base profile") {
		t.Errorf("unexpected error: %s", string(out))
	}
}
