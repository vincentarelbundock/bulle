package record

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/vincentarelbundock/bulle/internal/cli"
	"github.com/vincentarelbundock/bulle/internal/config"
	benv "github.com/vincentarelbundock/bulle/internal/env"
	"github.com/vincentarelbundock/bulle/internal/policy"
)

// learnedMarker is the first line of every profile file bulle writes for
// saved grants. A file that does not start with it is the user's own work and
// is never rewritten.
const learnedMarker = "# Saved by bulle. bulle rewrites this file when you save new grants;"

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

// PromptLearnedGrants runs the end-of-run save gate: it shows the entries that
// would allow what this run was denied, and offers to save them to the
// profile. It reports whether the run should be repeated. Only called on a
// terminal, and it never changes the exit code of the run.
func PromptLearnedGrants(opts cli.Options, global config.Config, rec *Recorder, scratch *ScratchRewrite, stdout, stderr io.Writer) bool {
	inScratch := scratch != nil
	if len(rec.Unsaved()) == 0 {
		return false
	}
	name, create := learnTargetProfile(opts, global)
	if name == "" {
		return false
	}
	// What is shown is what would be written, entry for entry. Generalization
	// and directory promotion happen before the prompt, not after it: a prompt
	// listing three files while the save writes the directory holding them is
	// consent obtained for something other than what happens.
	entries := learnedEntries(scratch.apply(rec.grants))
	fmt.Fprintln(stderr, "bulle: the sandbox denied accesses this run; the profile would receive:")
	for _, entry := range entries {
		line := fmt.Sprintf("  %-5s %s", entry.List, "?"+strings.TrimPrefix(entry.Entry, "?"))
		if entry.Comment != "" {
			line += "  (" + entry.Comment + ")"
		} else if entry.Denied != "" && entry.Denied != entry.Entry {
			line += "  (for " + entry.Denied + ")"
		}
		fmt.Fprintln(stderr, line)
	}
	action := "save these grants to profile"
	if create {
		action = "create profile"
	}
	prompt := fmt.Sprintf("%s %q?  [s]ave and run again  [w]rite and quit  [n]o: ", action, name)
	if inScratch {
		prompt = fmt.Sprintf("%s %q?  [w]rite  [n]o: ", action, name)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(stderr, prompt)
		answer, err := reader.ReadString('\n')
		if err != nil {
			return false
		}
		choice := strings.TrimSpace(answer)
		switch choice {
		case "s", "w":
			if inScratch && choice == "s" {
				continue
			}
			path, err := saveLearnedGrants(opts, global, name, create, entries)
			if err != nil {
				fmt.Fprintf(stderr, "bulle: cannot save: %v\n", err)
				fmt.Fprintln(stderr, "bulle: add the grants above to the profile yourself")
				return false
			}
			rec.MarkSaved()
			fmt.Fprintf(stderr, "bulle: saved to %s\n", path)
			return choice == "s"
		case "n", "":
			return false
		}
	}
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

// learnedFile is the part of a profile file bulle manages: the grant lists and
// the identity fields the creation case writes. Everything else the user put in
// the file is carried across a rewrite verbatim — see keptKeys.
type learnedFile struct {
	Title       string   `toml:"title,omitempty"`
	Description string   `toml:"description,omitempty"`
	Inherits    []string `toml:"inherits,omitempty"`
	DefaultApp  string   `toml:"default_app,omitempty"`
	Ro          []string `toml:"ro,omitempty"`
	Rox         []string `toml:"rox,omitempty"`
	Rw          []string `toml:"rw,omitempty"`
	Rwx         []string `toml:"rwx,omitempty"`
	// kept holds every other top-level key the file had. The header the tool
	// writes invites hand-editing, and a rewrite that silently dropped a
	// deny = ["network"] the user added would turn an edit meant to tighten the
	// profile into one that loosens it.
	kept map[string]any
}

// learnedEntries turns the accumulated grants into the exact entries a save
// would write: generalized (variables substituted, store roots collapsed),
// merged, and promoted.
func learnedEntries(grants []Grant) []recordedEntry {
	g := newGeneralizer(recordVars(), policy.ListResolvers(os.Getenv("PATH"), benv.Parent()), exec.LookPath)
	entries := make([]recordedEntry, 0, len(grants))
	for _, gr := range grants {
		entries = append(entries, g.generalize(gr))
	}
	return finalizeEntries(entries)
}

// managedKeys are the top-level keys saveLearnedGrants renders itself.
var managedKeys = map[string]bool{
	"title": true, "description": true, "inherits": true, "default_app": true,
	"ro": true, "rox": true, "rw": true, "rwx": true,
}

// keptKeys extracts every top-level key the typed struct does not model, so a
// rewrite preserves it.
func keptKeys(data []byte) (map[string]any, error) {
	var all map[string]any
	if err := toml.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	kept := map[string]any{}
	for key, value := range all {
		if !managedKeys[key] {
			kept[key] = value
		}
	}
	return kept, nil
}

// saveLearnedGrants writes the accumulated grants to
// <config>/profiles/<name>.toml (every entry optional). The file merges into
// any same-named profile at load time, so an overlay on a built-in profile and
// a newly created profile are the same mechanism.
func saveLearnedGrants(opts cli.Options, global config.Config, name string, create bool, entries []recordedEntry) (string, error) {
	root := opts.Config
	if root == "" {
		root = config.DefaultRoot()
	}
	if root == "" {
		return "", fmt.Errorf("could not determine the configuration directory")
	}
	dir := filepath.Join(root, "profiles")
	path := filepath.Join(dir, name+".toml")

	var file learnedFile
	if data, err := os.ReadFile(path); err == nil {
		if !strings.HasPrefix(string(data), learnedMarker) {
			return "", fmt.Errorf("%s exists and was not written by bulle; refusing to rewrite it", path)
		}
		stripped := stripComments(data)
		if err := toml.Unmarshal(stripped, &file); err != nil {
			return "", fmt.Errorf("re-read %s: %w", path, err)
		}
		kept, err := keptKeys(stripped)
		if err != nil {
			return "", fmt.Errorf("re-read %s: %w", path, err)
		}
		file.kept = kept
	} else if !os.IsNotExist(err) {
		return "", err
	} else if create {
		file.Title = strings.ToUpper(name[:1]) + name[1:]
		file.Description = name + " (grants saved by bulle)"
		file.Inherits = []string{"default"}
		if len(opts.Command) > 0 {
			file.DefaultApp = opts.Command[0]
		}
	}

	for _, entry := range entries {
		list := listFor(&file, entry.List)
		*list = appendUnique(*list, "?"+strings.TrimPrefix(entry.Entry, "?"))
	}
	for _, list := range []*[]string{&file.Ro, &file.Rox, &file.Rw, &file.Rwx} {
		sort.Strings(*list)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(renderLearnedFile(file)), 0o644)
}

func listFor(file *learnedFile, list string) *[]string {
	switch list {
	case "rox":
		return &file.Rox
	case "rw":
		return &file.Rw
	case "rwx":
		return &file.Rwx
	default:
		return &file.Ro
	}
}

func appendUnique(list []string, entry string) []string {
	for _, existing := range list {
		if existing == entry {
			return list
		}
	}
	return append(list, entry)
}

// stripComments removes full-line comments so a hand-annotated but still
// marker-led file round-trips through the strict decoder.
func stripComments(data []byte) []byte {
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

func renderLearnedFile(file learnedFile) string {
	var b strings.Builder
	b.WriteString(learnedMarker + "\n")
	b.WriteString("# a saved Grant is evidence that one run needed it, not that it is safe.\n")
	b.WriteString("# Review, edit, or delete entries freely — bulle only ever adds to the lists.\n\n")
	if file.Title != "" {
		fmt.Fprintf(&b, "title = %q\n", file.Title)
	}
	if file.Description != "" {
		fmt.Fprintf(&b, "description = %q\n", file.Description)
	}
	if len(file.Inherits) > 0 {
		quoted := make([]string, len(file.Inherits))
		for i, name := range file.Inherits {
			quoted[i] = fmt.Sprintf("%q", name)
		}
		fmt.Fprintf(&b, "inherits = [%s]\n", strings.Join(quoted, ", "))
	}
	if file.DefaultApp != "" {
		fmt.Fprintf(&b, "default_app = %q\n", file.DefaultApp)
	}
	for _, list := range []struct {
		name    string
		entries []string
	}{{"ro", file.Ro}, {"rox", file.Rox}, {"rw", file.Rw}, {"rwx", file.Rwx}} {
		if len(list.entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s = [\n", list.name)
		for _, entry := range list.entries {
			fmt.Fprintf(&b, "  %q,\n", entry)
		}
		b.WriteString("]\n")
	}
	// Anything the user added by hand goes back at the end, after the arrays
	// above, so a rewrite never turns their edit into a deletion. Table headers
	// this emits are legal there because nothing bulle writes is a table.
	if len(file.kept) > 0 {
		if rest, err := toml.Marshal(file.kept); err == nil {
			b.WriteString("\n")
			b.Write(rest)
		}
	}
	return b.String()
}
