//go:build linux

package backends

import (
	"os"
	"path/filepath"
	"testing"

	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

func TestWritableDirectoryRightsAvoidDeviceAndReferRights(t *testing.T) {
	rights := fsRights(true, false, true)
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

func TestStableLandlockRuleNeverFollowsSymlinkAlias(t *testing.T) {
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	// An invalid ruleset descriptor would fail if the alias were opened and a
	// rule attempted. A nil result proves the symlink spelling was skipped;
	// policy resolution supplies the canonical target as a separate rule.
	if err := addStableLandlockPath(-1, alias, false, false); err != nil {
		t.Fatalf("symlink alias was followed while constructing Landlock rule: %v", err)
	}
}
