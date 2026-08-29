// Package pyparse extracts import statements from the Python code a diff adds.
//
// # Why a parser and not a regex
//
// PHASE1-NOTES.md R1: real repositories contain lines that look exactly like
// imports but are not. pypa/pip's test suite embeds Python source inside a
// textwrap.dedent string literal and asserts on pip's reaction to it:
//
//	runner.write_text(textwrap.dedent("""\
//	    import pip_unexpected_module_xyz
//	"""))
//
// A line scanner reports a hallucinated package. tree-sitter reports nothing,
// because the text sits under string_content -> string and there is no
// import_statement node in that subtree at all. That single case is the entire
// false-positive surface of the 96-PR known-good corpus, so this distinction
// is not a detail — it is the difference between hitting the 0-noise target
// and missing it.
//
// # What we parse, given only a diff
//
// A diff does not contain whole files, so we reconstruct the post-image of
// each hunk: its context and added lines, in order, which is a contiguous
// region of the real file. Parsing the whole hunk (not just the added lines)
// matters — the dedent case only reads correctly because the opening
// `textwrap.dedent("""` is present in the same hunk. We then report only
// imports that land on ADDED lines; context lines are parsed for structure,
// never reported.
//
// Known limitation, deliberately accepted for now: a hunk that begins in the
// middle of a string literal or docstring loses the opening delimiter, so its
// text can parse as code and yield a spurious import. The fix is to read the
// real file from a checkout (--repo-root), which is how production will run;
// it is not implemented yet because the benchmark corpus has no checkouts to
// test it against, and untested fallback code is worse than none.
package pyparse

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"github.com/reevsryn/safecommit/internal/diff"
)

// Import kinds. "relative" is split out because a relative import is
// first-party by construction and can never be a registry hallucination.
const (
	KindImport     = "import"      // import X / import X.Y / import X as Z
	KindFromImport = "from-import" // from X import Y
	KindRelative   = "relative"    // from . import Y / from .X import Y
)

// An Import is one import statement found on an added line.
type Import struct {
	Module string // module path as written: "os.path", "dateutil"
	Top    string // top-level component: "os", "dateutil" — what we resolve
	Kind   string
	File   string
	Line   int    // real post-image line number
	Raw    string // the source line, for the finding message
}

// An Extractor holds a reusable tree-sitter parser. Not safe for concurrent
// use; create one per goroutine.
type Extractor struct {
	parser *ts.Parser
}

func NewExtractor() *Extractor {
	p := ts.NewParser()
	p.SetLanguage(ts.NewLanguage(tspython.Language()))
	return &Extractor{parser: p}
}

func (e *Extractor) Close() { e.parser.Close() }

// IsPython reports whether a path is Python source we should parse.
func IsPython(path string) bool {
	return strings.HasSuffix(path, ".py") || strings.HasSuffix(path, ".pyi")
}

// FromFile extracts imports introduced by the added lines of one diff file.
func (e *Extractor) FromFile(f *diff.File) []Import {
	if f.NewPath == "" || f.IsBinary || !IsPython(f.NewPath) {
		return nil
	}
	var out []Import
	for i := range f.Hunks {
		out = append(out, e.fromHunk(f.NewPath, &f.Hunks[i])...)
	}
	return out
}

// fromHunk reconstructs one hunk's post-image text, parses it, and reports the
// imports that sit on added lines.
func (e *Extractor) fromHunk(path string, h *diff.Hunk) []Import {
	var (
		sb      strings.Builder
		lineNo  []int  // reconstructed row -> real post-image line
		isAdded []bool // reconstructed row -> was this line added?
	)
	for _, l := range h.Lines {
		if l.Kind == diff.Deleted {
			continue // not part of the post-image
		}
		sb.WriteString(l.Text)
		sb.WriteByte('\n')
		lineNo = append(lineNo, l.NewLine)
		isAdded = append(isAdded, l.Kind == diff.Added)
	}
	if len(lineNo) == 0 {
		return nil
	}

	src := []byte(sb.String())
	tree := e.parser.Parse(src, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	var out []Import
	walk(tree.RootNode(), func(n *ts.Node) {
		kind := n.Kind()
		if kind != "import_statement" && kind != "import_from_statement" {
			return
		}
		row := int(n.StartPosition().Row)
		if row < 0 || row >= len(lineNo) || !isAdded[row] {
			return // context line, or out of range: parsed for structure only
		}
		raw := strings.TrimSpace(lineText(src, row))
		for _, im := range parseImportNode(n, src) {
			im.File = path
			im.Line = lineNo[row]
			im.Raw = raw
			out = append(out, im)
		}
	})
	return out
}

// parseImportNode pulls the module name(s) out of one import node.
func parseImportNode(n *ts.Node, src []byte) []Import {
	var out []Import

	if n.Kind() == "import_from_statement" {
		mod := n.ChildByFieldName("module_name")
		if mod == nil {
			return nil
		}
		if mod.Kind() == "relative_import" {
			return []Import{{Module: text(mod, src), Kind: KindRelative}}
		}
		name := text(mod, src)
		return []Import{{Module: name, Top: topOf(name), Kind: KindFromImport}}
	}

	// import_statement: one or more names, each dotted_name or aliased_import.
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		switch c.Kind() {
		case "dotted_name":
			name := text(c, src)
			out = append(out, Import{Module: name, Top: topOf(name), Kind: KindImport})
		case "aliased_import":
			if inner := c.ChildByFieldName("name"); inner != nil {
				name := text(inner, src)
				out = append(out, Import{Module: name, Top: topOf(name), Kind: KindImport})
			}
		}
	}
	return out
}

// walk visits every node in the tree, including children of ERROR nodes: a
// hunk is often not a syntactically complete file, and a real import can still
// sit inside a partially-recovered region.
func walk(n *ts.Node, fn func(*ts.Node)) {
	fn(n)
	for i := uint(0); i < n.ChildCount(); i++ {
		walk(n.Child(i), fn)
	}
}

func text(n *ts.Node, src []byte) string {
	return string(src[n.StartByte():n.EndByte()])
}

func topOf(module string) string {
	if i := strings.IndexByte(module, '.'); i >= 0 {
		return module[:i]
	}
	return module
}

// lineText returns row `row` (0-based) of src.
func lineText(src []byte, row int) string {
	s := string(src)
	for i := 0; i < row; i++ {
		j := strings.IndexByte(s, '\n')
		if j < 0 {
			return ""
		}
		s = s[j+1:]
	}
	if j := strings.IndexByte(s, '\n'); j >= 0 {
		s = s[:j]
	}
	return s
}
