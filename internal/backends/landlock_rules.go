//go:build linux

package backends

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"github.com/vincentarelbundock/bulle/internal/policy"
	"golang.org/x/sys/unix"
)

func applyLandlockFilesystem(p policy.Policy) error {
	abi, err := llsyscall.LandlockGetABIVersion()
	if err != nil {
		return fmt.Errorf("detect Landlock ABI: %w", err)
	}
	if abi < 3 {
		return fmt.Errorf("Landlock ABI v3 or newer is required (detected v%d)", abi)
	}

	// V3 handles every filesystem right through TRUNCATE. Keeping this fixed at
	// V3 preserves Bulle's existing contract on newer kernels instead of
	// silently beginning to restrict rights introduced by future ABIs.
	const handledAccessFS = (uint64(1) << 15) - 1
	ruleset, err := llsyscall.LandlockCreateRuleset(&llsyscall.RulesetAttr{HandledAccessFS: handledAccessFS}, 0)
	if err != nil {
		return fmt.Errorf("create Landlock ruleset: %w", err)
	}
	defer syscall.Close(ruleset)

	for _, group := range []struct {
		paths      []string
		executable bool
		writable   bool
	}{
		{p.ReadOnlyExec, true, false},
		{p.ReadWriteExec, true, true},
		{p.ReadOnly, false, false},
		{p.ReadWrite, false, true},
	} {
		for _, path := range group.paths {
			if err := addStableLandlockPath(ruleset, path, group.executable, group.writable); err != nil {
				return err
			}
		}
	}
	if abi < 8 {
		// libpsx enumerates this directory after restricting the first thread so
		// it can apply the ruleset to the remaining Go runtime threads. Without
		// this narrow rule, Landlock blocks libpsx midway through enforcement.
		taskDir := fmt.Sprintf("/proc/%d/task", os.Getpid())
		if err := addStableLandlockAccess(ruleset, taskDir, llsyscall.AccessFSReadDir); err != nil {
			return err
		}
	}

	flags := uint32(0)
	if abi >= 7 {
		flags |= llsyscall.FlagRestrictSelfLogNewExecOn
	}
	if abi >= 8 {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			return fmt.Errorf("set no_new_privs: %w", err)
		}
		if err := llsyscall.LandlockRestrictSelf(ruleset, flags|llsyscall.FlagRestrictSelfTSync); err != nil {
			return fmt.Errorf("apply Landlock ruleset: %w", err)
		}
		return nil
	}
	if err := llsyscall.AllThreadsPrctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs on all threads: %w", err)
	}
	if err := llsyscall.AllThreadsLandlockRestrictSelf(ruleset, flags); err != nil {
		return fmt.Errorf("apply Landlock ruleset on all threads: %w", err)
	}
	return nil
}

func addStableLandlockAccess(ruleset int, path string, access uint64) error {
	how := &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, how)
	if err != nil {
		return fmt.Errorf("open stable Landlock path %q: %w", path, err)
	}
	defer unix.Close(fd)
	attr := llsyscall.PathBeneathAttr{ParentFd: fd, AllowedAccess: access}
	if err := llsyscall.LandlockAddPathBeneathRule(ruleset, &attr, 0); err != nil {
		return fmt.Errorf("add stable Landlock path %q: %w", path, err)
	}
	return nil
}

// addStableLandlockPath opens a path without following any symlink component
// and adds the rule using that already-open descriptor. Policy resolution also
// includes each symlink's canonical target, so skipping the alias is both
// compatible and immune to a repointing race between validation and sandbox
// entry.
func addStableLandlockPath(ruleset int, path string, executable bool, writable bool) error {
	how := &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, how)
	if errors.Is(err, unix.ELOOP) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open stable Landlock path %q: %w", path, err)
	}
	defer unix.Close(fd)

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat stable Landlock path %q: %w", path, err)
	}
	attr := llsyscall.PathBeneathAttr{
		ParentFd:      fd,
		AllowedAccess: fsRights(stat.Mode&unix.S_IFMT == unix.S_IFDIR, executable, writable),
	}
	if err := llsyscall.LandlockAddPathBeneathRule(ruleset, &attr, 0); err != nil {
		return fmt.Errorf("add stable Landlock path %q: %w", path, err)
	}
	return nil
}

func fsRights(dir bool, executable bool, writable bool) uint64 {
	access := uint64(llsyscall.AccessFSReadFile)
	if executable {
		access |= llsyscall.AccessFSExecute
	}
	if writable {
		access |= llsyscall.AccessFSWriteFile | llsyscall.AccessFSTruncate
	}
	if dir {
		access |= llsyscall.AccessFSReadDir
		if writable {
			access |= llsyscall.AccessFSRemoveDir | llsyscall.AccessFSRemoveFile
			access |= llsyscall.AccessFSMakeDir | llsyscall.AccessFSMakeReg
			access |= llsyscall.AccessFSMakeSock | llsyscall.AccessFSMakeFifo
			access |= llsyscall.AccessFSMakeSym
		}
	}
	return access
}
