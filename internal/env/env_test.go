package env

import "testing"

func TestResolveEnvironmentAllowlist(t *testing.T) {
	parent := map[string]string{
		"PATH":           "/usr/bin",
		"OPENAI_API_KEY": "secret",
		"GH_TOKEN":       "token",
	}
	got, err := Resolve(parent, []string{"PATH", "OPENAI_API_KEY", "NODE_ENV=development"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got["PATH"] != "/usr/bin" {
		t.Fatalf("PATH = %q", got["PATH"])
	}
	if got["OPENAI_API_KEY"] != "secret" {
		t.Fatalf("OPENAI_API_KEY not passed: %#v", got)
	}
	if _, ok := got["GH_TOKEN"]; ok {
		t.Fatalf("GH_TOKEN passed without being allowed")
	}
	if got["NODE_ENV"] != "development" {
		t.Fatalf("NODE_ENV = %q", got["NODE_ENV"])
	}
}

func TestResolvePrecedenceAndEmptyValues(t *testing.T) {
	parent := map[string]string{"FOO": "parent", "BAR": "parent"}
	got, err := Resolve(parent, []string{"FOO", "MISSING", "FOO=explicit", "BAR="})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got["FOO"] != "explicit" {
		t.Fatalf("FOO = %q", got["FOO"])
	}
	if got["BAR"] != "" {
		t.Fatalf("BAR = %q", got["BAR"])
	}
	if _, ok := got["MISSING"]; ok {
		t.Fatalf("MISSING passed without a parent value")
	}
}

func TestResolveRejectsInvalidNames(t *testing.T) {
	for _, item := range []string{"", "=value", "1BAD=value", "BAD-NAME=value", "BAD.NAME=value", "BAD NAME=value"} {
		t.Run(item, func(t *testing.T) {
			if _, err := Resolve(nil, []string{item}); err == nil {
				t.Fatalf("Resolve(%q) succeeded, want error", item)
			}
		})
	}
}

func TestResolveGlobEntries(t *testing.T) {
	parent := map[string]string{
		"GIT_AUTHOR":    "a",
		"GIT_EDITOR":    "e",
		"HOME":          "/home/x",
		"BASH_FUNC_f%%": "() { :; }",
	}
	out, err := Resolve(parent, []string{"GIT_*"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out["GIT_AUTHOR"] != "a" || out["GIT_EDITOR"] != "e" {
		t.Fatalf("out = %#v", out)
	}
	if _, ok := out["HOME"]; ok {
		t.Fatalf("HOME leaked: %#v", out)
	}
	out, err = Resolve(parent, []string{"BASH_*"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("invalid names should be skipped: %#v", out)
	}
}
