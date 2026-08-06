// Package finding defines SafeCommit's output contract.
//
// This is deliberately the SAME contract Phase 0 froze in corpus/README.md, so
// the existing eval harness grades this Go binary and the Python dummy tools
// without modification:
//
//	stdin  : a unified diff
//	stdout : a JSON array of findings, each {name, file?, line?, kind?, message?}
//	exit   : informational only — bench/runner.py grades stdout, because a real
//	         CI tool exits non-zero precisely *when* it has findings.
//
// `name` is the match key (bench/score.py matches findings to truths by
// PEP 503-normalized name). `file` and `line` are optional in Phase 0 scoring
// but SafeCommit populates them from the start: PHASE1-NOTES.md R2 requires
// tightening the scorer to file/line agreement before any number is published,
// and a detector that never recorded them could not be graded that way.
package finding

import (
	"encoding/json"
	"io"
)

// Kind values. These matter for grading, not just display: PHASE1-NOTES.md R3
// requires recall to be broken out per detection path, because import
// extraction and manifest parsing are different code paths and a blended
// number can hide one of them being completely broken.
const (
	KindImport      = "import"      // an `import X` / `from X import ...` statement
	KindRequirement = "requirement" // a dependency line in requirements.txt / pyproject.toml
)

// A Finding is one thing SafeCommit claims does not exist.
//
// The bar for emitting one is deliberately high: ground truth must prove
// non-existence. When we are merely unsure, we emit nothing — silence is the
// product's whole thesis.
type Finding struct {
	Name    string `json:"name"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
}

// Emit writes findings to w as a JSON array, one line.
//
// The nil check is not cosmetic. Go marshals a nil slice as `null`, but
// bench/score.py's parse_findings rejects anything that is not a JSON array
// ("tool output must be a JSON array of findings") — so the empty case, which
// is exactly what we expect on all 96 known-good cases, would be scored as a
// contract violation instead of a clean pass. The one case we most need to get
// right is the one Go's zero value would break.
func Emit(w io.Writer, fs []Finding) error {
	if fs == nil {
		fs = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // package names are not HTML; keep output readable
	return enc.Encode(fs)
}
