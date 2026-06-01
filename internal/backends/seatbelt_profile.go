package backends

import (
	"strings"

	"github.com/vincentarelbundock/bulle/internal/policy"
)

func BuildSeatbeltProfile(p policy.Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n\n")
	b.WriteString("(deny default)\n\n")
	b.WriteString("(allow process-exec*)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow sysctl-read)\n")
	readPaths := appendPaths(p.ReadOnly, p.ReadOnlyExec)
	readWritePaths := appendPaths(p.ReadWrite, p.ReadWriteExec)
	execPaths := appendPaths(p.ReadOnlyExec, p.ReadWriteExec)
	rootAncestors, nonRootAncestors := splitRoot(ancestorDirs(appendPaths(readPaths, readWritePaths)))
	writeLiteralRule(&b, "file-read-data file-read-metadata", rootAncestors)
	metadataOnlyPaths := append([]string{}, nonRootAncestors...)
	// macOS name resolution checks the /var symlink alias, and common
	// runtimes stat /tmp even when TMPDIR points somewhere narrower. Metadata
	// is enough for both cases and does not grant reads of temp file contents.
	metadataOnlyPaths = append(metadataOnlyPaths, "/var", "/tmp", "/private/tmp")
	writeLiteralRule(&b, "file-read-metadata", metadataOnlyPaths)
	writeSubpathRule(&b, "file-read-metadata", []string{"/tmp", "/private/tmp"})
	writeSeatbeltPathRules(&b, "file-read*", readPaths, false)
	writeSeatbeltPathRules(&b, "file-read* file-write*", readWritePaths, true)
	writeSeatbeltPathRules(&b, "file-read* file-map-executable", execPaths, false)
	// ttyname(3) and /usr/bin/tty need to read the /dev directory to map the
	// inherited terminal file descriptor back to /dev/ttysNNN. This does not
	// grant access to device nodes under /dev; those remain listed separately.
	writeLiteralRule(&b, "file-read-data file-read-metadata", []string{"/dev"})
	writeLiteralRule(&b, "file-read* file-write*", []string{"/dev/tty", "/dev/null"})
	writeLiteralRule(&b, "file-read*", []string{"/dev/urandom"})
	ttyPaths := ttyDevicePaths()
	writeLiteralRule(&b, "file-read* file-write*", ttyPaths)
	writeLiteralRule(&b, "file-ioctl", append([]string{"/dev/tty", "/dev/null"}, ttyPaths...))
	machLookups := []string{
		"com.apple.SystemConfiguration.DNSConfiguration",
		"com.apple.SystemConfiguration.configd",
		"com.apple.trustd.agent",
		"com.apple.system.opendirectoryd.libinfo",
	}
	if p.AllowKeychain {
		// allow_keychain gates macOS service access, not file access. The
		// Keychain database paths themselves belong in normal profile path
		// lists such as rw/rwx.
		machLookups = append(machLookups, "com.apple.SecurityServer", "com.apple.securityd", "com.apple.securityd.xpc")
	}
	writeMachLookupRule(&b, machLookups)
	if p.Network != policy.NetworkNone {
		b.WriteString("\n(allow network*)\n")
	}
	return b.String()
}
