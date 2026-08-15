package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
)

func TestRetryHintLine(t *testing.T) {
	hints := []string{
		"denied: read /home/u/.gitconfig — add --ro ~/.gitconfig",
		"denied: execute /opt/x — add --rox /opt/x",
		"denied: connect tcp — network access is restricted by the sandbox policy",
		"denied: read /home/u/.gitconfig — add --ro ~/.gitconfig",
	}
	got := retryHintLine(hints)
	want := "bulle: retry with these grants added: --ro ~/.gitconfig --rox /opt/x"
	if got != want {
		t.Fatalf("retry = %q, want %q", got, want)
	}
	if retryHintLine([]string{"denied: connect tcp — network access is restricted by the sandbox policy"}) != "" {
		t.Fatalf("network-only hints should produce no retry line")
	}
}

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
