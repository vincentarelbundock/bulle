package policy

import "testing"

func TestNewViewHidesEnvValues(t *testing.T) {
	view := NewView(Policy{Env: map[string]string{"OPENAI_API_KEY": "secret"}, AllowKeychain: true, Network: NetworkNone})
	if len(view.EnvKeys) != 1 || view.EnvKeys[0] != "OPENAI_API_KEY" {
		t.Fatalf("EnvKeys = %#v", view.EnvKeys)
	}
	if !view.AllowKeychain {
		t.Fatalf("AllowKeychain = false, want true")
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
