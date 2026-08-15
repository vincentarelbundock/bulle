package paths

import (
	"os"
	"path/filepath"
	"strconv"
)

// RuntimeTempRoot is bulle's per-user directory under a temporary root. The
// uid is in the name so two users on one machine never contend for the same
// path, which a shared /tmp would otherwise let them do.
func RuntimeTempRoot(base string) string {
	return filepath.Join(base, "bulle-"+strconv.Itoa(os.Getuid()))
}
