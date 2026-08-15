package cli

import (
	"strings"
	"testing"
)

func TestParseUnknownFlagSuggests(t *testing.T) {
	_, err := Parse([]string{"bulle", "--confi", "/tmp", "--", "true"})
	if err == nil || !strings.Contains(err.Error(), `did you mean "--config"?`) {
		t.Fatalf("err = %v", err)
	}
	// Nothing close: no suggestion, but a pointer to the flag list.
	_, err = Parse([]string{"bulle", "--zzzzzzz", "--", "true"})
	if err == nil || strings.Contains(err.Error(), "did you mean") || !strings.Contains(err.Error(), "flag list") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseFlagValueErrorShowsSyntax(t *testing.T) {
	_, err := Parse([]string{"bulle", "--memory"})
	if err == nil || !strings.Contains(err.Error(), "usage: --memory SIZE") {
		t.Fatalf("err = %v", err)
	}
}
