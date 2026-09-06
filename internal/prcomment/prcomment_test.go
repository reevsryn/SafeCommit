package prcomment

import (
	"strings"
	"testing"

	"github.com/reevsryn/safecommit/internal/finding"
)

func f(name, file string, line int, kind, msg string) finding.Finding {
	return finding.Finding{Name: name, File: file, Line: line, Kind: kind, Message: msg}
}

// Silence is the product. No findings must produce no comment at all -- not an
// empty one, and not a cheerful "all clear", which is exactly the noise this
// tool exists to avoid adding to a PR.
func TestNoFindingsRendersNothing(t *testing.T) {
	body, ok := Render(nil, Options{})
	if ok || body != "" {
		t.Errorf("got ok=%v body=%q, want no comment at all", ok, body)
	}
	if body, ok := Render([]finding.Finding{}, Options{}); ok || body != "" {
		t.Errorf("empty slice: got ok=%v body=%q", ok, body)
	}
}

func TestSingleFindingWording(t *testing.T) {
	body, ok := Render([]finding.Finding{
		f("reqursts", "svc/api.py", 12, finding.KindImport,
			`no PyPI distribution provides the import "reqursts" (PyPI 404 (checked 2026-09-06)).`),
	}, Options{})
	if !ok {
		t.Fatal("want a comment")
	}
	if !strings.Contains(body, "1 reference that does not exist") {
		t.Errorf("singular wording missing:\n%s", body)
	}
	if !strings.Contains(body, Marker) {
		t.Error("marker missing — re-runs would post duplicates instead of updating")
	}
	if !strings.Contains(body, "PyPI 404") {
		t.Errorf("evidence missing; the checkable ground truth is the differentiator:\n%s", body)
	}
	if !strings.Contains(body, "not uploaded") {
		t.Error("privacy statement missing")
	}
}

func TestFindingsAreSortedByFileThenLine(t *testing.T) {
	body, _ := Render([]finding.Finding{
		f("zeta", "b.py", 5, finding.KindImport, "(PyPI 404 (checked x))"),
		f("alpha", "a.py", 99, finding.KindImport, "(PyPI 404 (checked x))"),
		f("beta", "a.py", 3, finding.KindImport, "(PyPI 404 (checked x))"),
	}, Options{})
	ia := strings.Index(body, "beta")
	ib := strings.Index(body, "alpha")
	ic := strings.Index(body, "zeta")
	if !(ia < ib && ib < ic) {
		t.Errorf("expected a.py:3, a.py:99, b.py:5 order; got:\n%s", body)
	}
	if !strings.Contains(body, "3 references that do not exist") {
		t.Error("plural wording missing")
	}
}

func TestLinkBaseProducesLinks(t *testing.T) {
	body, _ := Render([]finding.Finding{
		f("nunpy", "pkg/mod.py", 7, finding.KindImport, "(PyPI 404 (checked x))"),
	}, Options{LinkBase: "https://github.com/o/r/blob/abc123/"})
	want := "[`pkg/mod.py:7`](https://github.com/o/r/blob/abc123/pkg/mod.py#L7)"
	if !strings.Contains(body, want) {
		t.Errorf("want link %q in:\n%s", want, body)
	}
}

func TestPlainTextWithoutLinkBase(t *testing.T) {
	body, _ := Render([]finding.Finding{
		f("nunpy", "pkg/mod.py", 7, finding.KindImport, "(PyPI 404 (checked x))"),
	}, Options{})
	if strings.Contains(body, "http") {
		t.Errorf("no LinkBase should mean no links:\n%s", body)
	}
	if !strings.Contains(body, "`pkg/mod.py:7`") {
		t.Errorf("plain location missing:\n%s", body)
	}
}

func TestRequirementAndImportReadDifferently(t *testing.T) {
	body, _ := Render([]finding.Finding{
		f("python-requests", "requirements.txt", 3, finding.KindRequirement,
			`dependency "python-requests" does not exist on PyPI (PyPI 404 (checked x)).`),
		f("nunpy", "a.py", 1, finding.KindImport,
			`no PyPI distribution provides the import "nunpy" (PyPI 404 (checked x)).`),
	}, Options{})
	if !strings.Contains(body, "no such distribution") {
		t.Errorf("manifest finding should read as a missing distribution:\n%s", body)
	}
	if !strings.Contains(body, "no distribution provides this import") {
		t.Errorf("import finding should read as an unprovided import:\n%s", body)
	}
}

// The comment must stay short enough that a reviewer reads it. One row per
// finding, no preamble padding.
func TestCommentStaysCompact(t *testing.T) {
	var fs []finding.Finding
	for i := 0; i < 3; i++ {
		fs = append(fs, f("pkg", "a.py", i+1, finding.KindImport, "(PyPI 404 (checked x))"))
	}
	body, _ := Render(fs, Options{Version: "0.2.0"})
	if n := strings.Count(body, "\n"); n > 16 {
		t.Errorf("comment is %d lines for 3 findings; brevity is the feature:\n%s", n, body)
	}
	if !strings.Contains(body, "safecommit 0.2.0") {
		t.Error("version footer missing")
	}
}
