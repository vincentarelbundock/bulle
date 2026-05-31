package backends

import (
	"strconv"
	"strings"
)

func writeLiteralRule(b *strings.Builder, rights string, paths []string) {
	if len(paths) == 0 {
		return
	}
	b.WriteString("\n(allow ")
	b.WriteString(rights)
	for _, path := range paths {
		b.WriteString("\n  (literal ")
		b.WriteString(strconv.Quote(path))
		b.WriteString(")")
	}
	b.WriteString(")\n")
}

func writeSubpathRule(b *strings.Builder, rights string, paths []string) {
	if len(paths) == 0 {
		return
	}
	b.WriteString("\n(allow ")
	b.WriteString(rights)
	for _, path := range paths {
		b.WriteString("\n  (subpath ")
		b.WriteString(strconv.Quote(path))
		b.WriteString(")")
	}
	b.WriteString(")\n")
}

func writeRegexRule(b *strings.Builder, rights string, patterns []string) {
	if len(patterns) == 0 {
		return
	}
	b.WriteString("\n(allow ")
	b.WriteString(rights)
	for _, pattern := range patterns {
		b.WriteString("\n  (regex ")
		b.WriteString(strconv.Quote(pattern))
		b.WriteString(")")
	}
	b.WriteString(")\n")
}

func writeMachLookupRule(b *strings.Builder, names []string) {
	if len(names) == 0 {
		return
	}
	b.WriteString("\n(allow mach-lookup")
	for _, name := range names {
		b.WriteString("\n  (global-name ")
		b.WriteString(strconv.Quote(name))
		b.WriteString(")")
	}
	b.WriteString(")\n")
}

func writeSeatbeltPathRules(b *strings.Builder, rights string, paths []string, scratchFiles bool) {
	files, dirs := splitPathTypes(paths)
	writeLiteralRule(b, rights, appendPaths(files, dirs))
	writeSubpathRule(b, rights, dirs)
	if scratchFiles {
		writeRegexRule(b, rights, writableFileScratchPatterns(files))
	}
}
