package pyparse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reevsryn/safecommit/internal/diff"
)

func extractFile(t *testing.T, diffSrc string) []Import {
	t.Helper()
	files, err := diff.Parse([]byte(diffSrc))
	if err != nil {
		t.Fatalf("diff.Parse: %v", err)
	}
	e := NewExtractor()
	defer e.Close()
	var out []Import
	for i := range files {
		out = append(out, e.FromFile(&files[i])...)
	}
	return out
}

func extractCorpus(t *testing.T, rel string) []Import {
	t.Helper()
	src, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	files, err := diff.Parse(src)
	if err != nil {
		t.Fatalf("diff.Parse(%s): %v", rel, err)
	}
	e := NewExtractor()
	defer e.Close()
	var out []Import
	for i := range files {
		out = append(out, e.FromFile(&files[i])...)
	}
	return out
}

func tops(ims []Import) map[string]bool {
	m := map[string]bool{}
	for _, i := range ims {
		m[i.Top] = true
	}
	return m
}

func TestBasicImportForms(t *testing.T) {
	got := extractFile(t, `diff --git a/a.py b/a.py
new file mode 100644
--- /dev/null
+++ b/a.py
@@ -0,0 +1,6 @@
+import os
+import os.path
+import numpy as np
+import json, csv
+from dateutil import parser
+from . import sibling
`)
	want := []struct {
		mod, top, kind string
		line           int
	}{
		{"os", "os", KindImport, 1},
		{"os.path", "os", KindImport, 2},
		{"numpy", "numpy", KindImport, 3},
		{"json", "json", KindImport, 4},
		{"csv", "csv", KindImport, 4},
		{"dateutil", "dateutil", KindFromImport, 5},
		{".", "", KindRelative, 6},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d imports, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Module != w.mod || got[i].Top != w.top || got[i].Kind != w.kind || got[i].Line != w.line {
			t.Errorf("import %d: got %+v, want {%s %s %s line %d}", i, got[i], w.mod, w.top, w.kind, w.line)
		}
	}
}

// The R1 case, synthetic. A regex reports two imports here; the parser must
// report one.
func TestImportInsideStringLiteralIsNotExtracted(t *testing.T) {
	got := extractFile(t, `diff --git a/t.py b/t.py
new file mode 100644
--- /dev/null
+++ b/t.py
@@ -0,0 +1,7 @@
+import textwrap
+
+def test_it(runner):
+    runner.write_text(textwrap.dedent("""\
+        import totally_fake_package
+        """))
+
`)
	if tops(got)["totally_fake_package"] {
		t.Errorf("extracted an import from inside a string literal: %+v", got)
	}
	if !tops(got)["textwrap"] {
		t.Errorf("missed the real import; got %+v", got)
	}
}

// The R1 case, REAL. pypa/pip PR 13912 is the single candidate in the entire
// 96-PR known-good corpus that survives the resolution cascade. If the parser
// extracts it, the 0-noise target is unreachable.
func TestRealPipStringFixtureYieldsNoFakeImport(t *testing.T) {
	got := extractCorpus(t, filepath.Join("..", "..", "corpus", "known-good", "diffs", "pypa__pip__pr13912.diff"))
	if tops(got)["pip_unexpected_module_xyz"] {
		t.Fatalf("extracted pip_unexpected_module_xyz — this is THE known-good false positive; got %+v", got)
	}
	t.Logf("pip PR13912: %d imports extracted, none of them the string-literal fixture", len(got))
}

// R1 case 2, REAL: psf/black PR 5161 adds `from m import (...)` in a formatter
// TEST-DATA file. It is a genuine import statement even to a parser, so this
// test documents that parsing does NOT solve case 2 — path-aware suppression
// must, and that lands in step 5. If this ever starts passing "by accident",
// the suppression logic changed and R1 case 2 needs re-checking.
func TestBlackTestDataImportIsStillExtracted(t *testing.T) {
	got := extractCorpus(t, filepath.Join("..", "..", "corpus", "known-good", "diffs", "psf__black__pr5161.diff"))
	if !tops(got)["m"] {
		t.Skip("black PR5161 no longer contains the `from m import` fixture; re-check R1 case 2")
	}
	t.Logf("as expected, parsing alone still extracts the test-data import `m` (R1 case 2 needs path suppression)")
}

func TestSeededCaseIsExtracted(t *testing.T) {
	got := extractCorpus(t, filepath.Join("..", "..", "corpus", "seeded", "diffs", "seed-019.diff"))
	var found *Import
	for i := range got {
		if got[i].Top == "dateutil" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("did not extract the dateutil import; got %+v", got)
	}
	if found.File != "reports/weekly.py" || found.Line != 2 {
		t.Errorf("got %s:%d, want reports/weekly.py:2", found.File, found.Line)
	}
}

func TestContextLinesAreNotReported(t *testing.T) {
	// `import os` is context (unchanged); only `import requests` was added.
	got := extractFile(t, `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,2 +1,3 @@
 import os
+import requests
 x = 1
`)
	if len(got) != 1 || got[0].Top != "requests" {
		t.Fatalf("got %+v, want only the added `requests` import", got)
	}
	if got[0].Line != 2 {
		t.Errorf("line = %d, want 2", got[0].Line)
	}
}

func TestNonPythonFilesAreSkipped(t *testing.T) {
	got := extractFile(t, `diff --git a/requirements.txt b/requirements.txt
--- a/requirements.txt
+++ b/requirements.txt
@@ -1,1 +1,2 @@
 flask==3.0.3
+import-looking-line
`)
	if len(got) != 0 {
		t.Errorf("got %+v, want none (manifest parsing is a separate path, R3)", got)
	}
}

// Extraction must survive every real diff without panicking.
func TestExtractRealCorpusDoesNotPanic(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "corpus", "known-good", "diffs"),
		filepath.Join("..", "..", "corpus", "seeded", "diffs"),
	}
	e := NewExtractor()
	defer e.Close()
	total, imports := 0, 0
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("reading %s: %v", root, err)
		}
		for _, ent := range entries {
			if filepath.Ext(ent.Name()) != ".diff" {
				continue
			}
			src, err := os.ReadFile(filepath.Join(root, ent.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files, err := diff.Parse(src)
			if err != nil {
				t.Errorf("%s: %v", ent.Name(), err)
				continue
			}
			total++
			for i := range files {
				for _, im := range e.FromFile(&files[i]) {
					imports++
					if im.Line <= 0 {
						t.Errorf("%s: import with no line: %+v", ent.Name(), im)
					}
					if im.Kind != KindRelative && im.Top == "" {
						t.Errorf("%s: non-relative import with empty Top: %+v", ent.Name(), im)
					}
				}
			}
		}
	}
	t.Logf("extracted %d imports from %d diffs", imports, total)
}
