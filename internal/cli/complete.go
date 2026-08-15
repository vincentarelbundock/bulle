package cli

import (
	"strings"

	"github.com/vincentarelbundock/bulle/internal/config"
)

// Directive tells the completion shim what to do beyond the returned
// candidates. The values follow the cobra __complete wire protocol, so the
// shims are the standard shape.
type Directive int

const (
	// DirectiveDefault lets the shell add its own file completion.
	DirectiveDefault Directive = 0
	// DirectiveNoFile means the candidates are exhaustive.
	DirectiveNoFile Directive = 4
)

// Complete returns completion candidates for a partial command line. words is
// the argument list after the program name, with the word under the cursor
// last (possibly empty). Candidates may carry a tab-separated description,
// which shims strip or display depending on the shell. Everything is answered
// from the same tables the parser and dispatcher use — GlobalFlags derives
// from the Flags struct, commands is the dispatch table — so completion
// cannot drift from the real CLI.
func Complete(cfg config.Config, commands []CommandSpec, words []string) ([]string, Directive) {
	if len(words) == 0 {
		words = []string{""}
	}
	cur := words[len(words)-1]
	prior := words[:len(words)-1]

	// Past the -- separator everything belongs to the sandboxed command;
	// leave it to the shell.
	for _, word := range prior {
		if word == "--" {
			return nil, DirectiveDefault
		}
	}

	flags := GlobalFlags()
	var sub *CommandSpec
	if len(prior) > 0 {
		for i := range commands {
			if commands[i].Name == prior[0] {
				sub = &commands[i]
				flags = append(flags, sub.Extra...)
				break
			}
		}
	}
	lookup := func(word string) *FlagSpec {
		for i := range flags {
			f := &flags[i]
			if word == "--"+f.Name || (f.Short != "" && word == "-"+f.Short) {
				return f
			}
		}
		return nil
	}

	// The word before the cursor is a value-taking flag: complete its value.
	if len(prior) > 0 {
		if f := lookup(prior[len(prior)-1]); f != nil && f.TakesValue {
			return completeValue(cfg, f, cur, "")
		}
	}
	// --flag=partial completes the value in place, keeping the prefix.
	if strings.HasPrefix(cur, "--") {
		if name, val, ok := strings.Cut(cur, "="); ok {
			if f := lookup(name); f != nil && f.TakesValue {
				return completeValue(cfg, f, val, name+"=")
			}
			return nil, DirectiveNoFile
		}
	}
	if strings.HasPrefix(cur, "-") {
		var out []string
		for _, f := range flags {
			name := "--" + f.Name
			if strings.HasPrefix(name, cur) {
				out = append(out, name+"\t"+f.Help)
			}
		}
		return out, DirectiveNoFile
	}

	var out []string
	if sub == nil && len(prior) == 0 {
		for _, c := range commands {
			if !c.Hidden && strings.HasPrefix(c.Name, cur) {
				out = append(out, c.Name)
			}
		}
		// The first positional is a profile (or comma-separated profiles);
		// complete the segment after the last comma.
		names, _ := completeProfileSegment(cfg, cur, "")
		out = append(out, names...)
		return out, DirectiveNoFile
	}
	if sub != nil && len(sub.Verbs) > 0 && !verbChosen(*sub, prior[1:]) {
		for _, verb := range sub.Verbs {
			if strings.HasPrefix(verb, cur) {
				out = append(out, verb)
			}
		}
		// scratch and show also accept a run's grammar, so the profile slot
		// completes there too.
		if sub.Name == "scratch" || sub.Name == "show" {
			names, _ := completeProfileSegment(cfg, cur, "")
			out = append(out, names...)
			return out, DirectiveNoFile
		}
	}
	// Later positionals are the workspace directory or the sandboxed command,
	// so the shell's file completion stays on.
	return out, DirectiveDefault
}

// completeProfileSegment completes a profile name inside a comma-separated
// merge list, keeping what came before the last comma.
func completeProfileSegment(cfg config.Config, cur string, prefix string) ([]string, Directive) {
	head := ""
	if i := strings.LastIndex(cur, ","); i >= 0 {
		head, cur = cur[:i+1], cur[i+1:]
	}
	var out []string
	for _, name := range ProfileNames(cfg) {
		if strings.HasPrefix(name, cur) {
			out = append(out, prefix+head+name+"\t"+profileDescription(name, cfg))
		}
	}
	return out, DirectiveNoFile
}

func verbChosen(sub CommandSpec, rest []string) bool {
	for _, word := range rest {
		for _, verb := range sub.Verbs {
			if word == verb {
				return true
			}
		}
	}
	return false
}

func completeValue(cfg config.Config, f *FlagSpec, cur string, prefix string) ([]string, Directive) {
	switch f.Complete {
	case "profile":
		return completeProfileSegment(cfg, cur, prefix)
	case "file":
		return nil, DirectiveDefault
	default:
		return nil, DirectiveNoFile
	}
}
