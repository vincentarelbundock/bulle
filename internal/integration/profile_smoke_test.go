package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// profileSmokeCase is one row of the profile verification table: a built-in
// profile, the tool it needs on PATH, and a command whose output proves the
// profile grants enough to run it. A profile does not ship without a row
// here; the CI matrix runs the table on Ubuntu, macOS, and Nix.
type profileSmokeCase struct {
	name    string
	profile string
	// requires is skipped-if-absent: smoke tests verify profiles against
	// tools the machine has, they do not install tools.
	requires string
	args     []string
	want     string
	// setup prepares unsandboxed state the sandboxed command needs (for
	// example, a synced project venv for offline uv runs). It runs in the
	// test's temporary workspace.
	setup func(t *testing.T, workspace string)
}

func profileSmokeTable() []profileSmokeCase {
	return []profileSmokeCase{
		{
			name:     "r-runs-and-sees-libraries",
			profile:  "r",
			requires: "Rscript",
			args:     []string{"Rscript", "-e", `stopifnot(nzchar(R.home())); cat("smoke-ok", length(.libPaths()))`},
			want:     "smoke-ok",
		},
		{
			name:     "r-loads-compiled-package",
			profile:  "r",
			requires: "Rscript",
			args:     []string{"Rscript", "-e", `suppressMessages(library(stats)); x <- lm(y ~ x, data.frame(x=1:9, y=(1:9)*2)); cat("smoke-ok", round(coef(x)[2]))`},
			want:     "smoke-ok 2",
		},
		{
			name:     "uv-runs-python-offline",
			profile:  "uv",
			requires: "uv",
			setup: func(t *testing.T, workspace string) {
				run(t, workspace, "uv", "init", "--no-workspace", "--name", "smoke")
				run(t, workspace, "uv", "sync")
			},
			args: []string{"uv", "run", "--offline", "python", "-c", `print("smoke-ok")`},
			want: "smoke-ok",
		},
		{
			name:     "uv-base-profile-denies-network",
			profile:  "uv",
			requires: "uv",
			setup: func(t *testing.T, workspace string) {
				run(t, workspace, "uv", "init", "--no-workspace", "--name", "smoke")
				run(t, workspace, "uv", "sync")
			},
			// The two backends deny the network at different syscalls: the
			// Linux seccomp filter rejects socket(2) outright, while seatbelt
			// lets the descriptor exist and denies the operations that put it
			// on the network. Binding it exercises the property both share and
			// needs no reachable peer, so the check stays hermetic.
			args: []string{"uv", "run", "--offline", "python", "-c",
				`import socket
try:
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.bind(("127.0.0.1", 0))
    s.listen(1)
    print("network-reachable")
except OSError:
    print("smoke-ok")`},
			want: "smoke-ok",
		},
		{
			name:     "git-init-and-commit",
			profile:  "git",
			requires: "git",
			args: []string{"sh", "-c",
				`git init -q && git -c user.name=smoke -c user.email=smoke@test commit -q --allow-empty -m smoke && echo smoke-ok`},
			want: "smoke-ok",
		},
		{
			name:     "node-runs",
			profile:  "node",
			requires: "node",
			args:     []string{"node", "-e", `console.log("smoke-ok")`},
			want:     "smoke-ok",
		},
		{
			name:     "go-builds-and-runs",
			profile:  "go",
			requires: "go",
			setup: func(t *testing.T, workspace string) {
				writeFile(t, workspace, "go.mod", "module smoke\n\ngo 1.22\n")
				writeFile(t, workspace, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"smoke-ok\") }\n")
			},
			args: []string{"go", "run", "."},
			want: "smoke-ok",
		},
		{
			name:     "pandoc-converts",
			profile:  "pandoc",
			requires: "pandoc",
			setup: func(t *testing.T, workspace string) {
				writeFile(t, workspace, "doc.md", "# Hi\n\nHello *world*.\n")
			},
			args: []string{"sh", "-c", "pandoc doc.md -o doc.html && echo smoke-ok"},
			want: "smoke-ok",
		},
		{
			name:     "latex-compiles-pdf",
			profile:  "latex",
			requires: "pdflatex",
			setup: func(t *testing.T, workspace string) {
				writeFile(t, workspace, "t.tex", "\\documentclass{article}\\begin{document}Hello TeX\\end{document}\n")
			},
			args: []string{"sh", "-c", "pdflatex -interaction=batchmode t.tex >/dev/null; test -f t.pdf && echo smoke-ok"},
			want: "smoke-ok",
		},
		{
			name:     "quarto-renders-html",
			profile:  "quarto",
			requires: "quarto",
			setup: func(t *testing.T, workspace string) {
				writeFile(t, workspace, "q.qmd", "---\ntitle: Smoke\n---\n\nHello **quarto**.\n")
			},
			args: []string{"quarto", "render", "q.qmd", "--to", "html"},
			want: "Output created",
		},
		{
			name:     "rust-builds-and-runs",
			profile:  "rust",
			requires: "cargo",
			setup: func(t *testing.T, workspace string) {
				run(t, workspace, "cargo", "init", "--vcs", "none", "--name", "smoke", ".")
			},
			args: []string{"cargo", "run", "-q", "--offline"},
			want: "Hello, world!",
		},
	}
}

func TestProfileSmoke(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bulle")
	buildBulleForIntegration(t, bin)
	for _, tc := range profileSmokeTable() {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.requires); err != nil {
				t.Skipf("%s not on PATH", tc.requires)
			}
			workspace := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, workspace)
			}
			args := append([]string{tc.profile, workspace, "--rw", workspace, "--"}, tc.args...)
			cmd := exec.Command(bin, args...)
			cmd.Dir = workspace
			cmd.Env = os.Environ()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("profile %s failed: %v\noutput:\n%s", tc.profile, err, string(out))
			}
			if !strings.Contains(string(out), tc.want) {
				t.Fatalf("profile %s output %q, want it to contain %q", tc.profile, string(out), tc.want)
			}
		})
	}
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v, output: %s", name, strings.Join(args, " "), err, string(out))
	}
}
