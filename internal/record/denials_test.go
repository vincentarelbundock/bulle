package record

import (
	"reflect"
	"testing"
)

func TestParseLandlockDenials(t *testing.T) {
	lines := []string{
		`audit: type=1423 audit(1729738800.268:30): domain=195ba459b blockers=fs.read_file path="/etc/shadow" dev="vda2" ino=1523541`,
		`audit: type=1423 audit(1729738800.269:31): domain=195ba459b blockers=fs.write_file,fs.truncate path="/home/user/notes.txt" dev="vda2" ino=99`,
		`audit: type=1423 audit(1729738800.270:32): domain=195ba459b blockers=net.connect_tcp daddr=127.0.0.1 dest=443`,
		`unrelated kernel line`,
	}
	denials := parseLandlockDenials(lines, 0)
	if len(denials) != 3 {
		t.Fatalf("got %d denials, want 3", len(denials))
	}
	if denials[0].Path != "/etc/shadow" || denials[0].Blockers[0] != "fs.read_file" {
		t.Errorf("unexpected first denial: %+v", denials[0])
	}
	if denials[0].Domain != "195ba459b" {
		t.Errorf("domain = %q, want 195ba459b", denials[0].Domain)
	}
	if !reflect.DeepEqual(denials[1].Blockers, []string{"fs.write_file", "fs.truncate"}) {
		t.Errorf("unexpected blockers: %+v", denials[1].Blockers)
	}
	if denials[2].Path != "" {
		t.Errorf("network denial should have no path: %+v", denials[2])
	}
}

func TestParseLandlockDenialsFiltersOldDmesgLines(t *testing.T) {
	lines := []string{
		`[  100.000000] audit: type=1423 audit(1.2:1): domain=a blockers=fs.read_file path="/old" dev="x" ino=1`,
		`[  200.000000] audit: type=1423 audit(3.4:2): domain=a blockers=fs.read_file path="/new" dev="x" ino=2`,
	}
	denials := parseLandlockDenials(lines, 150)
	if len(denials) != 1 || denials[0].Path != "/new" {
		t.Fatalf("expected only the recent denial, got %+v", denials)
	}
}

func TestParseLandlockDenialsUnescapesQuotedPaths(t *testing.T) {
	lines := []string{
		`audit: type=1423 audit(1.2:1): domain=a blockers=fs.read_file path="/tmp/we\"ird" dev="x" ino=1`,
	}
	denials := parseLandlockDenials(lines, 0)
	if len(denials) != 1 || denials[0].Path != `/tmp/we"ird` {
		t.Fatalf("unexpected denials: %+v", denials)
	}
}

func TestDenialHints(t *testing.T) {
	denials := []landlockDenial{
		{Blockers: []string{"fs.read_file"}, Path: "/home/user/.gitconfig"},
		{Blockers: []string{"fs.read_file"}, Path: "/home/user/.gitconfig"},
		{Blockers: []string{"fs.write_file", "fs.truncate"}, Path: "/etc/hosts"},
		{Blockers: []string{"fs.execute"}, Path: "/usr/bin/git"},
		{Blockers: []string{"fs.make_reg"}, Path: "/var/log/app"},
	}
	hints := denialHints(denials, "/home/user")
	want := []string{
		"denied: execute /usr/bin/git — add --rox /usr/bin/git",
		"denied: read /home/user/.gitconfig — add --ro ~/.gitconfig",
		"denied: write /etc/hosts — add --rw /etc/hosts",
		"denied: write /var/log/app — add --rw /var/log/app",
	}
	if !reflect.DeepEqual(hints, want) {
		t.Errorf("got %v, want %v", hints, want)
	}
}

func TestDenialHintsNetwork(t *testing.T) {
	denials := []landlockDenial{{
		Blockers: []string{"net.connect_tcp"},
		Raw:      `audit: type=1423 audit(1.2:3): domain=a blockers=net.connect_tcp daddr=127.0.0.1 dest=443`,
	}}
	hints := denialHints(denials, "")
	if len(hints) != 1 {
		t.Fatalf("got %v, want one network hint", hints)
	}
	want := "denied: connect tcp — network access is restricted by the sandbox policy (blockers=net.connect_tcp daddr=127.0.0.1 dest=443)"
	if hints[0] != want {
		t.Errorf("got %q, want %q", hints[0], want)
	}
}

func TestParseSeatbeltDenials(t *testing.T) {
	lines := []string{
		`2026-08-10 12:00:00.123456-0400  localhost kernel[0]: (Sandbox) Sandbox: cat(1234) deny(1) file-read-data /Users/v/.gitconfig`,
		`2026-08-10 12:00:00.200000-0400  localhost kernel[0]: (Sandbox) Sandbox: touch(1235) deny(1) file-write-create /Users/v/My Documents/out.txt`,
		`2026-08-10 12:00:00.300000-0400  localhost kernel[0]: (Sandbox) Sandbox: curl(1236) deny(1) network-outbound *:443`,
		`2026-08-10 12:00:00.400000-0400  localhost kernel[0]: (Sandbox) Sandbox: zsh(1237) deny(1) mach-lookup com.apple.something`,
		`unrelated log line`,
	}
	denials := parseSeatbeltDenials(lines)
	if len(denials) != 3 {
		t.Fatalf("got %d denials, want 3 (mach-lookup filtered): %+v", len(denials), denials)
	}
	if denials[0].Process != "cat" || denials[0].Operation != "file-read-data" || denials[0].Path != "/Users/v/.gitconfig" {
		t.Errorf("unexpected first denial: %+v", denials[0])
	}
	if denials[1].Path != "/Users/v/My Documents/out.txt" {
		t.Errorf("path with spaces mangled: %+v", denials[1])
	}
}

func TestSeatbeltHints(t *testing.T) {
	denials := []seatbeltDenial{
		{Process: "cat", Operation: "file-read-data", Path: "/Users/v/.gitconfig"},
		{Process: "cat", Operation: "file-read-metadata", Path: "/Users/v/.gitconfig"},
		{Process: "touch", Operation: "file-write-create", Path: "/etc/hosts"},
		{Process: "git", Operation: "process-exec", Path: "/usr/bin/git"},
		{Process: "curl", Operation: "network-outbound", Path: "*:443"},
		{Process: "cat", Operation: "file-read-data", Path: "<private>"},
	}
	hints := seatbeltHints(denials, "/Users/v")
	want := []string{
		"denied: execute /usr/bin/git (git) — add --rox /usr/bin/git",
		"denied: outbound to *:443 (curl) — network access is restricted by the sandbox policy",
		"denied: read /Users/v/.gitconfig (cat) — add --ro ~/.gitconfig",
		"denied: write /etc/hosts (touch) — add --rw /etc/hosts",
	}
	if !reflect.DeepEqual(hints, want) {
		t.Errorf("got %#v, want %#v", hints, want)
	}
}

func TestAbbreviateHome(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"/home/u/.gitconfig", "/home/u", "~/.gitconfig"},
		{"/home/u", "/home/u", "~"},
		{"/home/uv/.gitconfig", "/home/u", "/home/uv/.gitconfig"},
		{"/etc/hosts", "/home/u", "/etc/hosts"},
		{"/etc/hosts", "", "/etc/hosts"},
	}
	for _, c := range cases {
		if got := abbreviateHome(c.path, c.home); got != c.want {
			t.Errorf("abbreviateHome(%q, %q) = %q, want %q", c.path, c.home, got, c.want)
		}
	}
}

// macOS filenames may contain newlines, so a denied path can carry what looks
// like a second log line. Only lines that really are kernel log lines count.
func TestParseSeatbeltDenialsIgnoresFabricatedLines(t *testing.T) {
	lines := []string{
		`Sandbox: cat(1) deny(1) file-read-data /Users/v/.ssh/id_rsa`,
		`  (Sandbox) Sandbox: cat(1) deny(1) file-read-data /Users/v/.ssh/id_rsa`,
		`something else entirely`,
	}
	if got := parseSeatbeltDenials(lines); len(got) != 0 {
		t.Fatalf("parseSeatbeltDenials = %#v, want nothing from lines that are not kernel log lines", got)
	}
}
