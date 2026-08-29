// Package stdlib answers "is this top-level module part of the Python
// standard library?" — the cascade's cheapest and highest-volume suppressor.
//
// On the 96-PR known-good corpus, 156 of 428 import candidates (36%) are
// stdlib. Every one of them is a name we must never look up in a package
// registry, because most are not distributions at all: PyPI has no project
// called `os` or `dataclasses`, so a registry-first design would report the
// entire standard library as hallucinated.
//
// The list is generated from a real interpreter rather than hand-maintained;
// see gen.md. It errs toward INCLUDING names (it carries modules removed in
// recent versions, like distutils and imp) because the two errors are not
// symmetric: suppressing an extra name costs recall we do not actually have —
// nobody hallucinates `asynchat` — while missing one costs a false positive,
// which is the thing this project exists to avoid.
package stdlib

import (
	_ "embed"
	"strings"
)

//go:embed modules.txt
var modulesTxt string

var modules map[string]bool

func init() {
	modules = make(map[string]bool, 400)
	for _, line := range strings.Split(modulesTxt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		modules[line] = true
	}
}

// Is reports whether name is a top-level standard-library module.
// Matching is case-sensitive: Python module names are.
func Is(name string) bool { return modules[name] }

// Count returns how many modules are known. Used by tests to catch an embed
// that silently resolved to an empty file.
func Count() int { return len(modules) }
