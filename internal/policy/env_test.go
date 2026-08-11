package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestComposeEnvEntriesAllExcept(t *testing.T) {
	parent := map[string]string{"PATH": "/bin", "SECRET": "x", "TERM": "xterm", "BAD-NAME": "y"}
	entries, err := composeEnvEntries(nil, nil, parent, []string{"SECRET"}, nil, []string{"EXTRA=1"})
	if err != nil {
		t.Fatalf("composeEnvEntries: %v", err)
	}
	want := []string{"PATH", "TERM", "EXTRA=1"}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
}

func TestComposeEnvEntriesEnvFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.env")
	content := "# comment\n\nexport FOO=bar\nQUOTED=\"a b\"\nSINGLE='c d'\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := composeEnvEntries([]string{"HOME"}, nil, map[string]string{}, nil, []string{file}, nil)
	if err != nil {
		t.Fatalf("composeEnvEntries: %v", err)
	}
	want := []string{"HOME", "FOO=bar", "QUOTED=a b", "SINGLE=c d"}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
}

func TestReadEnvFileRejectsMalformedLine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(file, []byte("not a pair\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEnvFile(file); err == nil {
		t.Fatalf("readEnvFile succeeded, want malformed-line error")
	}
}
