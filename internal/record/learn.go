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

// PromptLearnedGrants runs the end-of-run save gate: it shows the grants that
// would allow what this run was denied, and offers to save them to the
// profile. It reports whether the run should be repeated. Only called on a
// terminal, and it never changes the exit code of the run.
func PromptLearnedGrants(opts cli.Options, global config.Config, rec *Recorder, inScratch bool, stdout, stderr io.Writer) bool {
	fresh := rec.Unsaved()
	if len(fresh) == 0 {
		return false
	}
	name, create := learnTargetProfile(opts, global)
	if name == "" {
		return false
	}
	fmt.Fprintln(stderr, "bulle: the sandbox denied accesses this run; grants that would allow them:")
	for _, gr := range fresh {
		line := fmt.Sprintf("  %-5s %s", strings.TrimPrefix(gr.Flag, "--"), gr.Path)
		if origins := rec.origins[gr]; len(origins) > 0 {
			line += "  (denied to " + strings.Join(origins, ", ") + ")"
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
			path, err := saveLearnedGrants(opts, global, name, create, rec.grants)
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

// learnedFile is the shape of a profile file bulle manages. It holds nothing
// but the Grant lists and the identity fields the creation case writes, so a
// rewrite loses nothing.
type learnedFile struct {
	Title       string   `toml:"title,omitempty"`
	Description string   `toml:"description,omitempty"`
	Inherits    []string `toml:"inherits,omitempty"`
	DefaultApp  string   `toml:"default_app,omitempty"`
	Ro          []string `toml:"ro,omitempty"`
	Rox         []string `toml:"rox,omitempty"`
	Rw          []string `toml:"rw,omitempty"`
	Rwx         []string `toml:"rwx,omitempty"`
}

// saveLearnedGrants writes the accumulated grants to
// <config>/profiles/<name>.toml, generalized the way record output was
// (variables substituted, store roots collapsed, every entry optional). The
// file merges into any same-named profile at load time, so an overlay on a
// built-in profile and a newly created profile are the same mechanism.
func saveLearnedGrants(opts cli.Options, global config.Config, name string, create bool, grants []Grant) (string, error) {
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
		if err := toml.Unmarshal(stripComments(data), &file); err != nil {
			return "", fmt.Errorf("re-read %s: %w", path, err)
		}
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

	g := newGeneralizer(recordVars(), policy.ListResolvers(os.Getenv("PATH"), benv.Parent()), exec.LookPath)
	entries := make([]recordedEntry, 0, len(grants))
	for _, gr := range grants {
		entries = append(entries, g.generalize(gr))
	}
	for _, entry := range finalizeEntries(entries) {
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
	return b.String()
}
