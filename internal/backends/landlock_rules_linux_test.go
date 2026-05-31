//go:build linux

package backends

import (
	"testing"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

func TestWritableDirectoryRightsAvoidDeviceAndReferRights(t *testing.T) {
	rights := fsRights(t.TempDir(), false, true)
	for name, bit := range map[string]uint64{
		"make_char":  llsyscall.AccessFSMakeChar,
		"make_block": llsyscall.AccessFSMakeBlock,
		"refer":      llsyscall.AccessFSRefer,
		"ioctl_dev":  llsyscall.AccessFSIoctlDev,
	} {
		if uint64(rights)&bit != 0 {
			t.Fatalf("writable directory rights include %s", name)
		}
	}
}
