package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestApplyConfigDefaults(t *testing.T) {
	defaults := config.DefaultsSettings{
		Profile: "claude",
		Timeout: "30s",
		Env:     []string{"GITHUB_TOKEN"},
		PathSettings: config.PathSettings{
			ReadOnly: []string{"?~/.gitconfig"},
		},
	}
	opts := cli.Options{}
	if err := applyConfigDefaults(&opts, defaults); err != nil {
		t.Fatalf("applyConfigDefaults: %v", err)
	}
	if opts.Profile != "claude" || opts.Timeout != 30*time.Second {
		t.Fatalf("opts = %+v", opts)
	}
	if !reflect.DeepEqual(opts.Env, []string{"GITHUB_TOKEN"}) || !reflect.DeepEqual(opts.ReadOnly, []string{"?~/.gitconfig"}) {
		t.Fatalf("opts lists = env %#v ro %#v", opts.Env, opts.ReadOnly)
	}

	explicit := cli.Options{Profile: "codex", Flags: cli.Flags{Timeout: "5s", Env: []string{"PATH"}}, Timeout: 5 * time.Second}
	if err := applyConfigDefaults(&explicit, defaults); err != nil {
		t.Fatalf("applyConfigDefaults: %v", err)
	}
	if explicit.Profile != "codex" || explicit.Timeout != 5*time.Second {
		t.Fatalf("explicit flags overridden: %+v", explicit)
	}
	if !reflect.DeepEqual(explicit.Env, []string{"GITHUB_TOKEN", "PATH"}) {
		t.Fatalf("env order = %#v", explicit.Env)
	}

	if err := applyConfigDefaults(&cli.Options{}, config.DefaultsSettings{Timeout: "bogus"}); err == nil {
		t.Fatalf("applyConfigDefaults accepted invalid timeout")
	}
}
