//go:build linux

package record

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vincentarelbundock/bulle/internal/backends"
	"github.com/vincentarelbundock/bulle/internal/exitcode"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// probeDenialLoggingCommand is an internal invocation used only by the
// recording precondition check.
const probeDenialLoggingCommand = "__probe-denial-logging"

// probeSettleAttempts and probeSettleDelay bound how long the parent waits for
// a denial record to reach the log. Records are written asynchronously, so a
// single immediate read would report "no logging" on a machine that logs
// perfectly well, only slightly late.
const (
	probeSettleAttempts = 4
	probeSettleDelay    = 400 * time.Millisecond
)

// IsDenialLoggingProbe reports whether these arguments are the internal probe.
func IsDenialLoggingProbe(args []string) bool {
	return len(args) == 4 && args[1] == probeDenialLoggingCommand
}

// RunDenialLoggingProbe is the child side of the check. The "restrict" stage
// applies a sandbox and execs the "write" stage, which attempts the access
// that must be denied.
//
// The exec matters: Landlock audit logging is enabled for subprocesses, so a
// probe that restricted itself and hit the denial without an exec in between
// would test a case no sandboxed run produces, and could report no logging on
// a machine that logs every real denial.
//
// What the sandbox withholds is write access, not read. Denying a read would
// mean granting the child only the few paths re-execution needs, and
// discovering those means resolving the dynamic loader and its libraries —
// which comes back empty on distributions without an ld.so cache, turning a
// logging check into an accidental test of library discovery that fails on
// exactly the machines it is supposed to reassure. Granting read and execute
// everywhere and write nowhere keeps the probe measuring only what it was
// asked to measure. The child writes one file in a temporary directory and
// exits, so the breadth buys nothing that could be misused.
func RunDenialLoggingProbe(args []string, stderr io.Writer) int {
	stage, target := args[2], args[3]
	switch stage {
	case "restrict":
		self, err := os.Executable()
		if err != nil {
			return exitcode.SandboxSetup
		}
		if err := backends.ApplyFilesystemRestrictions(policy.Policy{ReadOnlyExec: []string{"/"}}); err != nil {
			return exitcode.SandboxSetup
		}
		argv := []string{self, probeDenialLoggingCommand, "write", target}
		if err := syscall.Exec(self, argv, os.Environ()); err != nil {
			return exitcode.SandboxSetup
		}
		return exitcode.SandboxSetup
	case "write":
		if err := os.WriteFile(target, []byte("probe"), 0o600); err != nil {
			// Denied, as intended. The parent looks for the kernel's record of
			// it, not for this exit code.
			return exitcode.CommandFailed
		}
		return exitcode.OK
	}
	fmt.Fprintf(stderr, "bulle: %s is an internal invocation and cannot be used directly\n", probeDenialLoggingCommand)
	return exitcode.ConfigError
}

// denialLoggingWorks reports whether a denial this machine refuses actually
// reaches the kernel log. Capability checks are not enough: a kernel can
// advertise Landlock ABI v7 and have readable logs while auditing is disabled,
// in which case recording would observe nothing, converge immediately, and
// emit an empty profile that reads exactly like success.
//
// The probe is a real sandboxed run of a known-denied write, so what it
// measures is what recording depends on.
func denialLoggingWorks() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	dir, err := os.MkdirTemp("", probeDirPrefix)
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	// The target is granted no write access, so writing to it must be denied.
	target := filepath.Join(dir, "denied")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		return false
	}
	// EvalSymlinks because the kernel reports the resolved path, and the
	// temporary directory commonly sits under a symlinked /tmp.
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}

	start := time.Now()
	startUptime := currentUptime()
	ctx, cancel := context.WithTimeout(context.Background(), denialLogTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, probeDenialLoggingCommand, "restrict", target)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err == nil {
		// The write succeeded, so the sandbox did not withhold what it was
		// asked to withhold. Nothing about logging can be concluded, and
		// recording should not proceed on a machine where that is true.
		return false
	}

	for attempt := 0; attempt < probeSettleAttempts; attempt++ {
		lines, viaDmesg := readKernelLog(start)
		since := 0.0
		if viaDmesg {
			since = startUptime
		}
		for _, d := range parseLandlockDenials(lines, since) {
			if d.Path == target {
				return true
			}
		}
		time.Sleep(probeSettleDelay)
	}
	return false
}
