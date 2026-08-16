package policy

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bpaths "github.com/vincentarelbundock/bulle/internal/paths"
)

// toolResolverTimeout bounds a single registry query. Interpreters can be slow
// to start (R reads its startup files), but a resolver that has not answered in
// this long is hung, not busy.
const toolResolverTimeout = 5 * time.Second

type resolverFormat int

const (
	// formatSingle reads one path from the whole output.
	formatSingle resolverFormat = iota
	// formatLines reads one path per non-empty output line.
	formatLines
)

// toolResolver describes one <tool>:<aspect> entry. Adding support for a new
// tool is a new row here, not new resolution code: the two mechanics (search
// PATH, ask a tool where its own directories are) are already implemented.
//
// Argv is fixed by this table and can never be supplied by a profile. Profiles
// are installable from GitHub, so a resolver that ran profile-supplied commands
// would make installing a profile equivalent to running its author's code.
type toolResolver struct {
	tool        string
	aspect      string
	argv        []string
	format      resolverFormat
	description string
	// appBundleRoot rewrites a result that lives inside a macOS .app to the
	// bundle directory itself, and drops results that do not. The developer
	// directory is the only thing a tool will report, but the toolchain
	// validates its own installation by reading the bundle's Info.plist and
	// shared frameworks, which sit above it. Dropping the non-bundle case is
	// what makes a Command Line Tools install (no enclosing .app) resolve to
	// nothing rather than to /Library/Developer.
	appBundleRoot bool
}

var toolResolvers = []toolResolver{
	{
		tool: "r", aspect: "home",
		argv:        []string{"Rscript", "--no-init-file", "--no-site-file", "-e", `cat(R.home())`},
		format:      formatSingle,
		description: "R installation directory (R.home()), including etc/ and lib/",
	},
	{
		tool: "r", aspect: "prefix",
		argv:        []string{"Rscript", "--no-init-file", "--no-site-file", "-e", `cat(dirname(dirname(R.home())))`},
		format:      formatSingle,
		description: "R installation prefix, for builds whose bin/ sits beside R_HOME",
	},
	{
		tool: "r", aspect: "libs",
		argv:        []string{"Rscript", "--no-init-file", "--no-site-file", "-e", `cat(.libPaths(), sep="\n")`},
		format:      formatLines,
		description: "every directory on R's package search path (.libPaths())",
	},
	{
		tool: "r", aspect: "libs-user",
		argv:        []string{"Rscript", "--no-init-file", "--no-site-file", "-e", `cat(Sys.getenv("R_LIBS_USER"))`},
		format:      formatSingle,
		description: "R's writable user library (R_LIBS_USER)",
	},
	{
		tool: "uv", aspect: "cache",
		argv:        []string{"uv", "cache", "dir"},
		format:      formatSingle,
		description: "uv package cache",
	},
	{
		tool: "uv", aspect: "tools",
		argv:        []string{"uv", "tool", "dir"},
		format:      formatSingle,
		description: "uv tool installation directory",
	},
	{
		tool: "uv", aspect: "python",
		argv:        []string{"uv", "python", "dir"},
		format:      formatSingle,
		description: "uv-managed Python interpreters",
	},
	{
		tool: "tex", aspect: "dist",
		argv:        []string{"kpsewhich", "-var-value", "TEXMFDIST"},
		format:      formatSingle,
		description: "TeX distribution tree (TEXMFDIST)",
	},
	{
		tool: "tex", aspect: "sysvar",
		argv:        []string{"kpsewhich", "-var-value", "TEXMFSYSVAR"},
		format:      formatSingle,
		description: "TeX system-wide runtime tree (TEXMFSYSVAR: format files, font caches)",
	},
	{
		tool: "tex", aspect: "var",
		argv:        []string{"kpsewhich", "-var-value", "TEXMFVAR"},
		format:      formatSingle,
		description: "TeX per-user runtime tree (TEXMFVAR: generated formats and font caches)",
	},
	{
		tool: "tex", aspect: "config",
		argv:        []string{"kpsewhich", "-var-value", "TEXMFCONFIG"},
		format:      formatSingle,
		description: "TeX per-user configuration tree (TEXMFCONFIG)",
	},
	{
		tool: "tex", aspect: "home",
		argv:        []string{"kpsewhich", "-var-value", "TEXMFHOME"},
		format:      formatSingle,
		description: "personal texmf tree (TEXMFHOME)",
	},
	{
		tool: "quarto", aspect: "paths",
		argv:        []string{"quarto", "--paths"},
		format:      formatLines,
		description: "quarto's bin and share directories",
	},
	{
		tool: "go", aspect: "root",
		argv:        []string{"go", "env", "GOROOT"},
		format:      formatSingle,
		description: "Go installation directory (GOROOT)",
	},
	{
		tool: "go", aspect: "path",
		argv:        []string{"go", "env", "GOPATH"},
		format:      formatSingle,
		description: "Go workspace directory (GOPATH)",
	},
	{
		tool: "go", aspect: "modcache",
		argv:        []string{"go", "env", "GOMODCACHE"},
		format:      formatSingle,
		description: "Go module cache",
	},
	{
		tool: "xcode", aspect: "developer",
		argv:        []string{"xcode-select", "-p"},
		format:      formatSingle,
		description: "active macOS developer directory (xcode-select -p), holding the toolchain and SDKs",
	},
	{
		tool: "xcode", aspect: "app",
		argv:          []string{"xcode-select", "-p"},
		format:        formatSingle,
		appBundleRoot: true,
		description:   "Xcode application bundle enclosing the developer directory; empty for a Command Line Tools install",
	},
	{
		tool: "npm", aspect: "cache",
		argv:        []string{"npm", "config", "get", "cache"},
		format:      formatSingle,
		description: "npm package cache",
	},
}

// KnownResolverTools reports the tool namespaces the registry defines, so
// namespace validation and help output stay in sync with the table.
func KnownResolverTools() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range toolResolvers {
		if !seen[r.tool] {
			seen[r.tool] = true
			out = append(out, r.tool)
		}
	}
	sort.Strings(out)
	return out
}

// ResolverListing describes one registry row for --list-resolvers.
type ResolverListing struct {
	Entry       string
	Description string
	Outcome     string
	Paths       []string
}

// ListResolvers evaluates every registry entry against the parent PATH so the
// user can see what each one resolves to on this machine. A resolver that
// returns nothing is otherwise indistinguishable from one that works.
func ListResolvers(parentPATH string, parentEnv map[string]string) []ResolverListing {
	out := make([]ResolverListing, 0, len(toolResolvers))
	for _, r := range toolResolvers {
		listing := ResolverListing{Entry: r.tool + ":" + r.aspect, Description: r.description}
		paths, err := r.run(parentPATH, parentEnv)
		switch {
		case err != nil:
			listing.Outcome = "unavailable (" + err.Error() + ")"
		case len(paths) == 0:
			listing.Outcome = "no paths reported"
		default:
			listing.Outcome = "ok"
			listing.Paths = paths
		}
		out = append(out, listing)
	}
	return out
}

func findToolResolver(tool string, aspect string) (toolResolver, bool) {
	for _, r := range toolResolvers {
		if r.tool == tool && r.aspect == aspect {
			return r, true
		}
	}
	return toolResolver{}, false
}

func aspectsForTool(tool string) []string {
	out := []string{}
	for _, r := range toolResolvers {
		if r.tool == tool {
			out = append(out, r.aspect)
		}
	}
	sort.Strings(out)
	return out
}

// run executes the registry command and returns the absolute paths it reported.
// The command is looked up on the parent PATH explicitly rather than inherited
// from the process environment, so what runs is decided by the same PATH the
// rest of resolution uses.
func (r toolResolver) run(parentPATH string, parentEnv map[string]string) ([]string, error) {
	binary, err := lookParentPATH(r.argv[0], parentPATH)
	if err != nil {
		return nil, fmt.Errorf("%q not found on parent PATH", r.argv[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolResolverTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, r.argv[1:]...)
	cmd.Env = envSlice(parentEnv)
	// Resolvers run before any sandbox exists, so whatever they read is read
	// with the user's full authority. Several of them source files from the
	// current directory — R evaluates ./.Rprofile, npm reads ./.npmrc, go reads
	// ./go.env — and the current directory is normally the workspace, which is
	// exactly the untrusted thing the sandbox is being built for. Run them from
	// the filesystem root, which no unprivileged user can plant files in.
	cmd.Dir = string(filepath.Separator)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s timed out after %s", r.argv[0], toolResolverTimeout)
		}
		return nil, fmt.Errorf("%s exited with an error", r.argv[0])
	}
	paths := parseResolverOutput(stdout.String(), r.format)
	if r.appBundleRoot {
		paths = keepAppBundleRoots(paths)
	}
	// Every resolver's output is dropped through the same guard, not just the
	// aspects that report an installation prefix: a tool reads its own
	// configuration to answer, and that configuration is not always the user's
	// (an .npmrc in the workspace decides what "npm config get cache" prints).
	paths = dropSystemRoots(paths, parentEnv["HOME"])
	return paths, nil
}

// keepAppBundleRoots maps each path to the nearest enclosing .app directory,
// dropping paths that are not inside one. Results are deduplicated because
// several aspects of one bundle collapse to the same root.
func keepAppBundleRoots(paths []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		for dir := path; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
			if !strings.HasSuffix(dir, ".app") {
				continue
			}
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
			break
		}
	}
	return out
}

// dropSystemRoots removes results that would hand over a whole system tree.
// It shares pkgRootDenylist with the pkg: resolver so both answer "is this a
// system root?" the same way.
func dropSystemRoots(paths []string, home string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if pkgRootDenylist[path] || (home != "" && sameExistingPath(path, home)) {
			continue
		}
		// An ancestor of home is no narrower than home itself.
		if home != "" && strings.HasPrefix(filepath.Clean(home), strings.TrimSuffix(path, "/")+"/") {
			continue
		}
		out = append(out, path)
	}
	return out
}

func parseResolverOutput(out string, format resolverFormat) []string {
	fields := []string{}
	switch format {
	case formatSingle:
		fields = append(fields, strings.TrimSpace(out))
	case formatLines:
		for _, line := range strings.Split(out, "\n") {
			fields = append(fields, strings.TrimSpace(line))
		}
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, field := range fields {
		// Only absolute paths are usable as grants. Anything else is a tool
		// reporting an unset value, a relative default, or a diagnostic line.
		if field == "" || !filepath.IsAbs(field) {
			continue
		}
		clean := filepath.Clean(field)
		if !seen[clean] {
			seen[clean] = true
			paths = append(paths, clean)
		}
	}
	// Deterministic ordering keeps the resolved policy stable across runs even
	// if a tool reorders its output.
	sort.Strings(paths)
	return paths
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

// expandToolResolvers replaces <tool>:<aspect> entries in any grant list with
// the paths the tool reports. Unlike which:/pkg:, these are ordinary path
// grants and are valid in every list. Results are not granted directly: they
// are written back as literal paths and re-resolved by ResolveListTrace, so the
// symlink pairing and sensitive-target refusals apply to them exactly as they
// do to a hand-written path.
func expandToolResolvers(entries []string, list string, parentPATH string, parentEnv map[string]string) ([]string, []bpaths.Trace, error) {
	out := make([]string, 0, len(entries))
	traces := []bpaths.Trace{}
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		path, optional, create := bpaths.ParseMarkers(trimmed)
		tool, aspect, ok := bpaths.ResolverNamespace(path)
		if !ok || isExecResolverNamespace(tool) {
			out = append(out, entry)
			continue
		}
		resolver, known := findToolResolver(tool, aspect)
		if !known {
			return nil, nil, unknownResolverError(list, entry, tool, aspect)
		}
		trace := bpaths.Trace{Raw: entry, List: list, Source: bpaths.SourceUser}
		paths, err := resolver.run(parentPATH, parentEnv)
		if err != nil {
			if optional {
				trace.Outcome = "skipped (" + err.Error() + ")"
				traces = append(traces, trace)
				continue
			}
			return nil, nil, fmt.Errorf("%s entry %q: %v (mark it optional with a ? prefix)", list, entry, err)
		}
		if len(paths) == 0 {
			// A tool that reports no paths is not an error: R with an unset
			// R_LIBS_USER is a normal configuration. Record it so an empty
			// grant is visible rather than silent.
			trace.Outcome = "skipped (no paths reported)"
			traces = append(traces, trace)
			continue
		}
		// Markers describe what should happen to the paths the entry names, so
		// they survive expansion: "?uv:cache" stays optional and "+npm:cache"
		// still creates a directory a tool reports but has not made yet.
		for _, path := range paths {
			out = append(out, reapplyMarkers(path, optional, create))
		}
		trace.Outcome = "granted"
		trace.Paths = paths
		traces = append(traces, trace)
	}
	return out, traces, nil
}

// reapplyMarkers rewrites an expanded path in configured-entry form so the
// markers on the resolver entry keep their meaning. Everything a tool resolver
// reports is a directory, so a create marker always means CreateDir regardless
// of how it was spelled on the entry.
func reapplyMarkers(path string, optional bool, create string) string {
	out := path
	if create != bpaths.CreateNone {
		out = "+" + out + "/"
	}
	if optional {
		out = "?" + out
	}
	return out
}

func unknownResolverError(list string, entry string, tool string, aspect string) error {
	if aspects := aspectsForTool(tool); len(aspects) > 0 {
		return fmt.Errorf("%s entry %q: unknown aspect %q for tool %q; known aspects are %s",
			list, entry, aspect, tool, strings.Join(aspects, ", "))
	}
	return fmt.Errorf("%s entry %q: unknown resolver %q; known resolvers are %s, and the executable resolvers which: and pkg:",
		list, entry, tool, strings.Join(KnownResolverTools(), ", "))
}
