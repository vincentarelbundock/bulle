package policy

import (
	"testing"
	"time"
)

func TestNewViewHidesEnvValues(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{"OPENAI_API_KEY": "secret"}, MachLookup: []string{"com.apple.SecurityServer"}, Network: NetworkNone})
	if len(view.EnvKeys) != 1 || view.EnvKeys[0] != "OPENAI_API_KEY" {
		t.Fatalf("EnvKeys = %#v", view.EnvKeys)
	}
	if len(view.MachLookup) != 1 || view.MachLookup[0] != "com.apple.SecurityServer" {
		t.Fatalf("MachLookup = %#v", view.MachLookup)
	}
	if view.Network != NetworkNone {
		t.Fatalf("Network = %q, want none", view.Network)
	}
}

func TestNewViewDefaultsNetworkToFull(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{}})
	if view.Network != NetworkFull {
		t.Fatalf("Network = %q, want full", view.Network)
	}
}

func TestNewViewFormatsTimeout(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{}, Timeout: 90 * time.Second})
	if view.Timeout != "1m30s" {
		t.Fatalf("Timeout = %q, want 1m30s", view.Timeout)
	}
}

func TestNewViewOmitsZeroTimeout(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{}})
	if view.Timeout != "" {
		t.Fatalf("Timeout = %q, want empty", view.Timeout)
	}
}
