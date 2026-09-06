// Package pathrules classifies file paths that contain code which is DATA
// rather than code which RUNS.
//
// PHASE1-NOTES.md R1 case 2: psf/black's test suite contains whole .py files
// that exist as *input* to the formatter. They parse as genuine Python with
// genuine import statements, so tree-sitter cannot tell them apart from real
// code — only the path can.
//
// # The boundary, and why it is narrow on purpose
//
// R1 states the anti-overreach constraint explicitly: do NOT suppress all of
// tests/. Imports at the top of an *executed* test file run under pytest, and
// a hallucinated one fails CI — those are legitimate findings. The seeded
// corpus enforces this: seed-008 puts a fake import in tests/test_schemas.py
// and expects it to fire. Suppressing tests/ wholesale would silently drop it.
//
// So the discriminator is a data/fixture path COMPONENT, not a test root:
//
//	tests/data/cases/python315.py   -> data      (suppress)
//	tests/test_schemas.py           -> executed  (report)
package pathrules

import "strings"

// Directory names that unambiguously hold fixture data wherever they appear.
var fixtureDirs = map[string]bool{
	"testdata":      true,
	"test-data":     true,
	"test_data":     true,
	"fixtures":      true,
	"__snapshots__": true,
	"snapshots":     true,
}

// Roots under which a plain "data" component means test fixtures. Bare "data"
// is too common in application code (myapp/data/loader.py) to suppress on its
// own.
var testRoots = map[string]bool{
	"test":    true,
	"tests":   true,
	"testing": true,
}

// IsFixtureData reports whether path holds code that is data, not code that runs.
func IsFixtureData(path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return false // a top-level file is not fixture data
	}
	underTestRoot := testRoots[parts[0]]
	for i, p := range parts[:len(parts)-1] { // directory components only
		if fixtureDirs[p] {
			return true
		}
		if p == "data" && (underTestRoot || i > 0 && testRoots[parts[i-1]]) {
			return true
		}
	}
	return false
}
