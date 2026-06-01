package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareCommandExecutableAddsResolvedCommandWhenAddExecIsSet(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "bulle-test-tool")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	got, err := PrepareCommandExecutable(Policy{
		Command: []string{"bulle-test-tool", "--version"},
		AddExec: true,
		Env:     map[string]string{"PATH": binDir},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != binary {
		t.Fatalf("Command[0] = %q, want %q", got.Command[0], binary)
	}
	if !contains(got.ReadOnlyExec, binary) {
		t.Fatalf("ReadOnlyExec = %#v, want %q", got.ReadOnlyExec, binary)
	}
}

func TestPrepareCommandExecutableUsesPolicyPATH(t *testing.T) {
	root := t.TempDir()
	allowedBin := filepath.Join(root, "allowed", "bin")
	deniedBin := filepath.Join(root, "denied", "bin")
	for _, dir := range []string{allowedBin, deniedBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	allowedTool := filepath.Join(allowedBin, "bulle-test-tool")
	deniedTool := filepath.Join(deniedBin, "bulle-test-tool")
	for _, path := range []string{allowedTool, deniedTool} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", deniedBin)

	got, err := PrepareCommandExecutable(Policy{
		Command: []string{"bulle-test-tool"},
		AddExec: true,
		Env:     map[string]string{"PATH": allowedBin},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != allowedTool {
		t.Fatalf("Command[0] = %q, want policy PATH executable %q", got.Command[0], allowedTool)
	}
	if contains(got.ReadOnlyExec, deniedTool) {
		t.Fatalf("ReadOnlyExec = %#v, must not include parent PATH executable %q", got.ReadOnlyExec, deniedTool)
	}
}

func TestPrepareCommandExecutableUsesExecutableRootsWhenPolicyPATHMissing(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "bulle-test-tool")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := PrepareCommandExecutable(Policy{
		Command:      []string{"bulle-test-tool"},
		ReadOnlyExec: []string{binDir},
		Env:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != binary {
		t.Fatalf("Command[0] = %q, want executable root match %q", got.Command[0], binary)
	}
	if _, ok := got.Env["PATH"]; ok {
		t.Fatalf("Env[PATH] is set, want executable root lookup without exporting PATH")
	}
}

func TestPrepareCommandExecutableFallsBackToExecutableRootsWhenPolicyPATHMisses(t *testing.T) {
	root := t.TempDir()
	allowedBin := filepath.Join(root, "allowed", "bin")
	deniedBin := filepath.Join(root, "denied", "bin")
	for _, dir := range []string{allowedBin, deniedBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	allowedTool := filepath.Join(allowedBin, "bulle-test-tool")
	if err := os.WriteFile(allowedTool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := PrepareCommandExecutable(Policy{
		Command:      []string{"bulle-test-tool"},
		ReadOnlyExec: []string{allowedBin},
		Env:          map[string]string{"PATH": deniedBin},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != allowedTool {
		t.Fatalf("Command[0] = %q, want executable root fallback %q", got.Command[0], allowedTool)
	}
}

func TestPrepareCommandExecutableWithAddExecUsesParentPATHForLookupOnly(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "bulle-test-tool")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	got, err := PrepareCommandExecutable(Policy{
		Command: []string{"bulle-test-tool"},
		AddExec: true,
		Env:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != binary {
		t.Fatalf("Command[0] = %q, want parent PATH executable %q", got.Command[0], binary)
	}
	if !contains(got.ReadOnlyExec, binary) {
		t.Fatalf("ReadOnlyExec = %#v, want %q", got.ReadOnlyExec, binary)
	}
	if _, ok := got.Env["PATH"]; ok {
		t.Fatalf("Env[PATH] is set, want parent PATH lookup without exporting PATH")
	}
}

func TestPrepareCommandExecutableResolvesRelativeCommandFromProjectPath(t *testing.T) {
	root := t.TempDir()
	launcher := filepath.Join(root, "launcher")
	project := filepath.Join(root, "project")
	for _, dir := range []string{launcher, project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcherTool := filepath.Join(launcher, "bulle-test-tool")
	projectTool := filepath.Join(project, "bulle-test-tool")
	for _, path := range []string{launcherTool, projectTool} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(launcher); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	got, err := PrepareCommandExecutable(Policy{
		ProjectPath: project,
		Command:     []string{"./bulle-test-tool"},
		AddExec:     true,
		Env:         map[string]string{},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != projectTool {
		t.Fatalf("Command[0] = %q, want project executable %q", got.Command[0], projectTool)
	}
	if contains(got.ReadOnlyExec, launcherTool) {
		t.Fatalf("ReadOnlyExec = %#v, must not include launcher cwd executable %q", got.ReadOnlyExec, launcherTool)
	}
}

func TestPrepareCommandExecutableWithAddExecSanitizesPATHAfterAddingCommand(t *testing.T) {
	root := t.TempDir()
	allowedBin := filepath.Join(root, "allowed", "bin")
	deniedBin := filepath.Join(root, "denied", "bin")
	for _, dir := range []string{allowedBin, deniedBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	allowedTool := filepath.Join(allowedBin, "bulle-test-tool")
	if err := os.WriteFile(allowedTool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := PrepareCommandExecutable(Policy{
		Command: []string{"bulle-test-tool"},
		AddExec: true,
		Env:     map[string]string{"PATH": deniedBin + string(os.PathListSeparator) + allowedBin},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != allowedTool {
		t.Fatalf("Command[0] = %q, want %q", got.Command[0], allowedTool)
	}
	if pathListHas(got.Env["PATH"], deniedBin) {
		t.Fatalf("PATH = %q, must not keep denied directory %q", got.Env["PATH"], deniedBin)
	}
}

func pathListHas(value, want string) bool {
	for _, path := range filepath.SplitList(value) {
		if path == want {
			return true
		}
	}
	return false
}

func TestPrepareCommandExecutableRejectsSymlinkEscapeWithoutAddExec(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outsideBin := filepath.Join(root, "outside", "bin")
	for _, path := range []string{allowed, outsideBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	escapingBin := filepath.Join(allowed, "bin")
	if err := os.Symlink(outsideBin, escapingBin); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(escapingBin, "bulle-test-tool")
	if err := os.WriteFile(filepath.Join(outsideBin, "bulle-test-tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareCommandExecutable(Policy{
		Command:      []string{tool},
		ReadOnlyExec: []string{allowed},
		Env:          map[string]string{},
	})
	if err == nil {
		t.Fatalf("PrepareCommandExecutable succeeded, want symlink escape rejection")
	}
}

func TestPrepareCommandExecutableWithAddExecAllowsExecutableRootSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	allowedBin := filepath.Join(root, "allowed", "bin")
	outsideBin := filepath.Join(root, "outside", "bin")
	for _, path := range []string{allowedBin, outsideBin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(outsideBin, "bulle-test-tool")
	link := filepath.Join(allowedBin, "bulle-test-tool")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := PrepareCommandExecutable(Policy{
		Command:      []string{"bulle-test-tool"},
		AddExec:      true,
		ReadOnlyExec: []string{allowedBin},
		Env:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("PrepareCommandExecutable returned error: %v", err)
	}
	if got.Command[0] != link {
		t.Fatalf("Command[0] = %q, want symlink path %q", got.Command[0], link)
	}
	for _, want := range []string{link, target} {
		if !contains(got.ReadOnlyExec, want) {
			t.Fatalf("ReadOnlyExec = %#v, want %q", got.ReadOnlyExec, want)
		}
	}
}
