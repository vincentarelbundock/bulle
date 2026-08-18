package record

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	benv "github.com/vincentarelbundock/bulle/internal/env"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// ScratchRewrite maps paths inside a --scratch workspace back to the origin
// they were cloned from. A scratch directory is per-run and gone tomorrow, so a
// grant recorded against it would be written into a permanent profile as a path
// that resolves nowhere — one stale entry per scratch run.
type ScratchRewrite struct {
	Dir    string
	Origin string
}

func (s *ScratchRewrite) apply(grants []Grant) []Grant {
	if s == nil || s.Dir == "" || s.Origin == "" {
		return grants
	}
	out := make([]Grant, len(grants))
	for i, gr := range grants {
		switch {
		case gr.Path == s.Dir:
			gr.Path = s.Origin
		case strings.HasPrefix(gr.Path, s.Dir+string(filepath.Separator)):
			gr.Path = s.Origin + strings.TrimPrefix(gr.Path, s.Dir)
		}
		out[i] = gr
	}
	return out
}

// ReportLearnedGrants prints what the run was denied, as the entries a
// profile would need. It never writes anything and never asks: a denial is
// evidence that one run wanted an access, not that granting it is safe, and
// that judgement is the user's to make in their own editor.
//
// What is printed is the generalized form — variables restored, package store
// roots collapsed, per-process paths folded — not the literal path the kernel
// reported. A run denied forty files under one cache directory is one line
// naming the directory, and a line spelled ?$HOME/... keeps meaning the right
// thing on another machine.
func ReportLearnedGrants(opts cli.Options, global config.Config, rec *Recorder, scratch *ScratchRewrite, stderr io.Writer) {
	if len(rec.grants) == 0 {
		return
	}
	entries := learnedEntries(scratch.apply(rec.grants))
	if len(entries) == 0 {
		return
	}
	// No count: one line here can stand for forty denials under one directory,
	// so any number printed beside them is the wrong one.
	name, _ := learnTargetProfile(opts, global)
	switch path := learnedProfilePath(opts, name); {
	case name == "":
		fmt.Fprintln(stderr, "bulle: the sandbox denied accesses; the run wanted:")
	case path != "":
		fmt.Fprintf(stderr, "bulle: the sandbox denied accesses; add to %q (%s):\n", name, path)
	default:
		fmt.Fprintf(stderr, "bulle: the sandbox denied accesses; add to profile %q:\n", name)
	}
	for _, entry := range entries {
		fmt.Fprintf(stderr, "  %-5s %s\n", entry.List, "?"+strings.TrimPrefix(entry.Entry, "?"))
	}
}

// learnedProfilePath names the file to paste the entries into: a profile file
// under the configuration directory merges into the same-named profile at
// load time, so this is the spelling that works for a built-in profile and a
// new one alike. Rendered with ~ because it is meant to be read, not parsed.
func learnedProfilePath(opts cli.Options, name string) string {
	root := opts.Config
	if root == "" {
		root = config.DefaultRoot()
	}
	if root == "" {
		return ""
	}
	path := filepath.Join(root, "profiles", name+".toml")
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// learnTargetProfile names the profile a save writes to: the first profile of
// the run, or — when the run had no profile — a new profile named after the
// command. The bool reports whether the profile would be newly created.
func learnTargetProfile(opts cli.Options, global config.Config) (string, bool) {
	if opts.Profile != "" {
		name, _, _ := strings.Cut(opts.Profile, ",")
		return strings.TrimSpace(name), false
	}
	if len(opts.Command) == 0 {
		return "", false
	}
	name := filepath.Base(opts.Command[0])
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", false
	}
	_, exists := global.Profiles[name]
	return name, !exists
}

// learnedEntries turns the accumulated grants into profile entries:
// generalized (variables substituted, store roots collapsed), merged, and
// promoted to the directory that covers them.
func learnedEntries(grants []Grant) []recordedEntry {
	g := newGeneralizer(recordVars(), policy.ListResolvers(os.Getenv("PATH"), benv.Parent()), exec.LookPath)
	entries := make([]recordedEntry, 0, len(grants))
	for _, gr := range grants {
		entries = append(entries, g.generalize(gr))
	}
	return finalizeEntries(entries)
}
