//go:build linux

package backends

import (
	"os"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

func applyLandlockFilesystem(p policy.Policy) error {
	llCfg := landlock.V3
	// On kernels with Landlock audit support (ABI v7, Linux 6.15+), ask the
	// kernel to log denials hit by the sandboxed command after execve. The
	// supervisor reads these back on failure to suggest policy fixes; they are
	// also visible via journalctl/dmesg. Enforcement stays strict V3 either way.
	if abi, err := llsyscall.LandlockGetABIVersion(); err == nil && abi >= 7 {
		llCfg = llCfg.EnableLoggingForSubprocesses()
	}
	rules := []landlock.Rule{}
	for _, path := range p.ReadOnlyExec {
		rules = append(rules, landlock.PathAccess(fsRights(path, true, false), path))
	}
	for _, path := range p.ReadWriteExec {
		rules = append(rules, landlock.PathAccess(fsRights(path, true, true), path))
	}
	for _, path := range p.ReadOnly {
		rules = append(rules, landlock.PathAccess(fsRights(path, false, false), path))
	}
	for _, path := range p.ReadWrite {
		rules = append(rules, landlock.PathAccess(fsRights(path, false, true), path))
	}
	if len(rules) == 0 {
		return llCfg.Restrict()
	}
	return llCfg.RestrictPaths(rules...)
}

func fsRights(path string, executable bool, writable bool) landlock.AccessFSSet {
	info, err := os.Stat(path)
	dir := err == nil && info.IsDir()
	access := landlock.AccessFSSet(0)
	access |= landlock.AccessFSSet(llsyscall.AccessFSReadFile)
	if executable {
		access |= landlock.AccessFSSet(llsyscall.AccessFSExecute)
	}
	if writable {
		access |= landlock.AccessFSSet(llsyscall.AccessFSWriteFile)
		access |= landlock.AccessFSSet(llsyscall.AccessFSTruncate)
	}
	if dir {
		access |= landlock.AccessFSSet(llsyscall.AccessFSReadDir)
		if writable {
			access |= landlock.AccessFSSet(llsyscall.AccessFSRemoveDir)
			access |= landlock.AccessFSSet(llsyscall.AccessFSRemoveFile)
			access |= landlock.AccessFSSet(llsyscall.AccessFSMakeDir)
			access |= landlock.AccessFSSet(llsyscall.AccessFSMakeReg)
			access |= landlock.AccessFSSet(llsyscall.AccessFSMakeSock)
			access |= landlock.AccessFSSet(llsyscall.AccessFSMakeFifo)
			access |= landlock.AccessFSSet(llsyscall.AccessFSMakeSym)
		}
	}
	return access
}
