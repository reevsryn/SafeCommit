package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLineMapping(t *testing.T) {
	// One added line between two context lines. The added line must land on
	// post-image line 2, and the context line after it must shift to 3.
	src := `diff --git a/requirements.txt b/requirements.txt
--- a/requirements.txt
+++ b/requirements.txt
@@ -1,2 +1,3 @@
 flask==3.0.3
+dateutil>=2.9
 gunicorn==22.0.0
`
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.NewPath != "requirements.txt" {
		t.Errorf("NewPath = %q, want requirements.txt", f.NewPath)
	}

	want := []Line{
		{Kind: Context, Text: "flask==3.0.3", OldLine: 1, NewLine: 1},
		{Kind: Added, Text: "dateutil>=2.9", OldLine: 0, NewLine: 2},
		{Kind: Context, Text: "gunicorn==22.0.0", OldLine: 2, NewLine: 3},
	}
	got := f.Hunks[0].Lines
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDeletionsDoNotAdvanceNewCursor(t *testing.T) {
	// The classic off-by-one: a '-' line must not consume a post-image number.
	// Counts are 2/2, not 3/3: the context line counts toward both totals, the
	// deleted line toward old only, the added line toward new only. (Getting
	// this wrong in the fixture is how this test first failed — the parser was
	// right and the hand-written diff was malformed.)
	src := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,2 +1,2 @@
 import os
-import sys
+import json
`
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	added := files[0].AddedLines()
	if len(added) != 1 {
		t.Fatalf("got %d added lines, want 1", len(added))
	}
	if added[0].Text != "import json" || added[0].NewLine != 2 {
		t.Errorf("got %+v, want {import json, NewLine:2}", added[0])
	}
	// The deleted line occupies old line 2 and no new line.
	del := files[0].Hunks[0].Lines[1]
	if del.Kind != Deleted || del.OldLine != 2 || del.NewLine != 0 {
		t.Errorf("deleted line = %+v, want {Deleted, OldLine:2, NewLine:0}", del)
	}
}

func TestNewFile(t *testing.T) {
	src := `diff --git a/reports/weekly.py b/reports/weekly.py
new file mode 100644
--- /dev/null
+++ b/reports/weekly.py
@@ -0,0 +1,2 @@
+"""Weekly report."""
+from dateutil import parser
`
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	f := files[0]
	if !f.IsNew {
		t.Error("IsNew = false, want true")
	}
	if f.OldPath != "" {
		t.Errorf("OldPath = %q, want empty", f.OldPath)
	}
	added := f.AddedLines()
	if len(added) != 2 || added[1].NewLine != 2 {
		t.Fatalf("added = %+v, want 2 lines ending at NewLine 2", added)
	}
}

func TestDeletedFile(t *testing.T) {
	src := `diff --git a/gone.py b/gone.py
deleted file mode 100644
--- a/gone.py
+++ /dev/null
@@ -1,1 +0,0 @@
-import os
`
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !files[0].IsDeleted {
		t.Error("IsDeleted = false, want true")
	}
	if files[0].NewPath != "" {
		t.Errorf("NewPath = %q, want empty (nothing to report against)", files[0].NewPath)
	}
	if len(files[0].AddedLines()) != 0 {
		t.Error("a deleted file must contribute no added lines")
	}
}

func TestOmittedCountsMeanOne(t *testing.T) {
	// "@@ -1 +1 @@" is legal shorthand for "@@ -1,1 +1,1 @@".
	src := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1 +1 @@
-import sys
+import json
`
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	h := files[0].Hunks[0]
	if h.OldCount != 1 || h.NewCount != 1 {
		t.Errorf("counts = %d/%d, want 1/1", h.OldCount, h.NewCount)
	}
}

func TestNoNewlineMarkerIsNotCounted(t *testing.T) {
	src := "diff --git a/a.py b/a.py\n" +
		"--- a/a.py\n+++ b/a.py\n" +
		"@@ -1 +1 @@\n" +
		"-import sys\n" +
		"\\ No newline at end of file\n" +
		"+import json\n" +
		"\\ No newline at end of file\n"
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(files[0].Hunks[0].Lines); got != 2 {
		t.Errorf("got %d lines, want 2 (markers excluded)", got)
	}
}

func TestEmptyContextLine(t *testing.T) {
	// A blank context line is sometimes emitted as "" rather than " ".
	src := "diff --git a/a.py b/a.py\n" +
		"--- a/a.py\n+++ b/a.py\n" +
		"@@ -1,3 +1,4 @@\n" +
		" import os\n" +
		"\n" +
		"+import json\n" +
		" x = 1\n"
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	added := files[0].AddedLines()
	if len(added) != 1 || added[0].NewLine != 3 {
		t.Fatalf("added = %+v, want one line at NewLine 3", added)
	}
}

func TestTruncatedHunkIsAnError(t *testing.T) {
	// Header promises 3 new lines; only 1 is present. Silently accepting this
	// would mean every later line number in the file is wrong.
	src := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,3 +1,3 @@
 import os
`
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("want error on truncated hunk, got nil")
	}
}

func TestMultipleFilesAndHunks(t *testing.T) {
	src := `diff --git a/a.py b/a.py
--- a/a.py
+++ b/a.py
@@ -1,1 +1,2 @@
 import os
+import json
@@ -10,1 +11,2 @@
 x = 1
+y = 2
diff --git a/b.py b/b.py
--- a/b.py
+++ b/b.py
@@ -5,1 +5,2 @@
 z = 3
+w = 4
`
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if len(files[0].Hunks) != 2 {
		t.Errorf("file 0: got %d hunks, want 2", len(files[0].Hunks))
	}
	// The second hunk starts at new line 11, so its added line is 12.
	a := files[0].AddedLines()
	if len(a) != 2 || a[0].NewLine != 2 || a[1].NewLine != 12 {
		t.Errorf("added = %+v, want NewLines 2 and 12", a)
	}
	if files[1].NewPath != "b.py" {
		t.Errorf("file 1 NewPath = %q, want b.py", files[1].NewPath)
	}
}

// TestParseRealCorpus is the test that matters: every diff in both real
// corpora must parse, with hunk arithmetic that checks out. These are real
// merged PRs from aiohttp, django, httpx, pandas, black, pip, pytest and
// scikit-learn — renames, binary files, mode changes, huge files and all.
func TestParseRealCorpus(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "corpus", "known-good", "diffs"),
		filepath.Join("..", "..", "corpus", "seeded", "diffs"),
	}
	total, addedTotal := 0, 0
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("reading %s: %v", root, err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".diff") {
				continue
			}
			path := filepath.Join(root, e.Name())
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			files, err := Parse(src)
			if err != nil {
				t.Errorf("%s: %v", e.Name(), err)
				continue
			}
			total++
			for i := range files {
				f := &files[i]
				for _, l := range f.AddedLines() {
					addedTotal++
					if l.NewLine <= 0 {
						t.Errorf("%s: added line without post-image number: %+v", e.Name(), l)
					}
					if l.OldLine != 0 {
						t.Errorf("%s: added line has OldLine %d, want 0", e.Name(), l.OldLine)
					}
					if f.NewPath == "" {
						t.Errorf("%s: added line in a file with no post-image path", e.Name())
					}
				}
			}
		}
	}
	if total != 116 {
		t.Errorf("parsed %d diffs, want 116 (96 known-good + 20 seeded)", total)
	}
	t.Logf("parsed %d diffs, %d added lines, all with valid post-image numbers", total, addedTotal)
}
