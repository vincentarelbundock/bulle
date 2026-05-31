package backends

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func TestPreparePolicyAddsShebangInterpreterWhenAddExecIsSet(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	interpreter := filepath.Join(binDir, "interp")
	if err := os.WriteFile(interpreter, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "script")
	if err := os.WriteFile(script, []byte("#!"+interpreter+" --flag\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, backend := range []policy.BackendName{policy.BackendLinuxLandlock, policy.BackendMacOSSeatbelt} {
		t.Run(string(backend), func(t *testing.T) {
			got, err := PreparePolicy(policy.Policy{
				Backend: backend,
				Command: []string{script},
				AddExec: true,
				Env:     map[string]string{},
			})
			if err != nil {
				t.Fatalf("PreparePolicy returned error: %v", err)
			}
			if !containsString(got.ReadOnlyExec, script) {
				t.Fatalf("ReadOnlyExec = %#v, want script %q", got.ReadOnlyExec, script)
			}
			if !containsString(got.ReadOnlyExec, interpreter) {
				t.Fatalf("ReadOnlyExec = %#v, want interpreter %q", got.ReadOnlyExec, interpreter)
			}
		})
	}
}

func TestPreparePolicyRejectsDisallowedShebangInterpreterWithoutAddExec(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	denied := filepath.Join(root, "denied")
	for _, dir := range []string{allowed, denied} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	interpreter := filepath.Join(denied, "interp")
	if err := os.WriteFile(interpreter, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(allowed, "script")
	if err := os.WriteFile(script, []byte("#!"+interpreter+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, backend := range []policy.BackendName{policy.BackendLinuxLandlock, policy.BackendMacOSSeatbelt} {
		t.Run(string(backend), func(t *testing.T) {
			_, err := PreparePolicy(policy.Policy{
				Backend:      backend,
				Command:      []string{script},
				ReadOnlyExec: []string{allowed},
				Env:          map[string]string{},
			})
			if err == nil {
				t.Fatalf("PreparePolicy succeeded, want disallowed interpreter error")
			}
		})
	}
}

func TestPreparePolicyAddsResolvedExecutable(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "bulle-test-tool")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	got, err := PreparePolicy(policy.Policy{
		Backend: policy.RuntimeDefaultBackend(),
		Command: []string{"bulle-test-tool", "--version"},
		AddExec: true,
		Env:     map[string]string{"PATH": binDir},
	})
	if err != nil {
		t.Fatalf("PreparePolicy returned error: %v", err)
	}
	if got.Command[0] != binary {
		t.Fatalf("Command[0] = %q, want %q", got.Command[0], binary)
	}
	if !containsString(got.ReadOnlyExec, binary) {
		t.Fatalf("ReadOnlyExec = %#v, want %q", got.ReadOnlyExec, binary)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
