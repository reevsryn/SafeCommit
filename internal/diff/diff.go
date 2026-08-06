// Package diff parses unified diffs into files, hunks, and lines carrying
// real post-image line numbers.
//
// # Why this exists when bench/diffscan.py already scans diffs
//
// diffscan.py answers only "which lines were added to which file". It has no
// line numbers, and its own docstring says so. PHASE1-NOTES.md R2 requires
// findings to carry accurate file+line so the scorer can be tightened from
// name-only matching to file/line agreement before any number is published.
// A finding that says "package X does not exist" without pointing at the line
// is not reviewable in a PR either. So this is a real parser, not a port.
//
// # The format, briefly (the thing to actually understand)
//
// A unified diff is a sequence of file sections. Each begins with a header:
//
//	diff --git a/old.py b/new.py
//	index 1111111..2222222 100644
//	--- a/old.py          <- pre-image path, or /dev/null when the file is new
//	+++ b/new.py          <- post-image path, or /dev/null when deleted
//
// then one or more hunks, each introduced by a hunk header:
//
//	@@ -oldStart,oldCount +newStart,newCount @@ optional section heading
//
// The counts are omitted when they are 1 (`@@ -1 +1 @@`). Following the header
// come exactly oldCount lines prefixed ' ' or '-', and exactly newCount lines
// prefixed ' ' or '+'. Context lines (' ') count toward BOTH totals — that
// dual-counting is the whole trick to line mapping:
//
//	walk the hunk, holding two cursors (old and new) starting at oldStart and
//	newStart. A context line advances both. A '-' advances only old. A '+'
//	advances only new — and its post-image line number is the new cursor.
//
// Because the header declares the counts up front, we can verify our walk:
// if the lines we consumed do not match oldCount/newCount exactly, either the
// diff is malformed or our parser is wrong. Parse treats that as an error
// rather than guessing, so bugs surface on real data instead of silently
// producing findings that point at the wrong line.
package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// LineKind distinguishes the three line roles inside a hunk.
type LineKind uint8

const (
	Context LineKind = iota // ' ' — present in both images
	Added                   // '+' — present only after the change
	Deleted                 // '-' — present only before the change
)

// A Line is one line inside a hunk, with its resolved line numbers.
//
// OldLine is 0 for added lines (they do not exist in the pre-image) and
// NewLine is 0 for deleted lines. Both are 1-based, matching how editors,
// GitHub, and humans count.
type Line struct {
	Kind    LineKind
	Text    string // content with the leading +/-/space prefix removed
	OldLine int
	NewLine int
}

// A Hunk is one contiguous changed region.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Section            string // the optional text after the closing @@
	Lines              []Line
}

// A File is one file's section of a diff.
//
// NewPath is the path to use for reporting: it is where the code lives after
// the change, which is what a reviewer sees and what we must resolve against
// the repo. It is empty when the file was deleted.
type File struct {
	OldPath   string
	NewPath   string
	IsNew     bool
	IsDeleted bool
	IsBinary  bool
	Hunks     []Hunk
}

// AddedLines returns every added line across all hunks, in file order.
// This is the input to candidate extraction.
func (f *File) AddedLines() []Line {
	var out []Line
	for i := range f.Hunks {
		for _, l := range f.Hunks[i].Lines {
			if l.Kind == Added {
				out = append(out, l)
			}
		}
	}
	return out
}

// Parse parses a unified diff. It is strict about hunk arithmetic (see the
// package doc) and lenient about everything it does not need: mode changes,
// index lines, similarity scores and binary payloads are recognised and
// skipped rather than rejected.
func Parse(src []byte) ([]File, error) {
	var (
		files   []File
		cur     *File
		sc      = bufio.NewScanner(bytes.NewReader(src))
		lineNum int
	)
	// Real-world diffs contain very long lines (minified files, data fixtures).
	// The default 64KiB token limit would fail on them; the corpus has files
	// approaching 100KB.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	flush := func() {
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for sc.Scan() {
		line := sc.Text()
		lineNum++

		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &File{}
			// Paths from the "diff --git a/x b/y" line are a fallback; the
			// ---/+++ headers below are authoritative when present.
			if o, n, ok := parseDiffGitPaths(line); ok {
				cur.OldPath, cur.NewPath = o, n
			}

		case cur == nil:
			// Preamble (commit message, "From ..." headers). Ignore.
			continue

		case strings.HasPrefix(line, "new file mode "):
			cur.IsNew = true
		case strings.HasPrefix(line, "deleted file mode "):
			cur.IsDeleted = true
		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
			cur.IsBinary = true

		case strings.HasPrefix(line, "--- "):
			p := strings.TrimPrefix(line, "--- ")
			if p == "/dev/null" {
				cur.IsNew = true
				cur.OldPath = ""
			} else {
				cur.OldPath = stripPathPrefix(p)
			}

		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if p == "/dev/null" {
				cur.IsDeleted = true
				cur.NewPath = ""
			} else {
				cur.NewPath = stripPathPrefix(p)
			}

		case strings.HasPrefix(line, "@@"):
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNum, err)
			}
			if err := readHunkBody(sc, &h, &lineNum); err != nil {
				return nil, fmt.Errorf("hunk at line %d (%s): %w", lineNum, cur.NewPath, err)
			}
			cur.Hunks = append(cur.Hunks, h)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading diff: %w", err)
	}
	flush()
	return files, nil
}

// readHunkBody consumes exactly the lines the header declared, assigning
// old/new line numbers as it walks.
func readHunkBody(sc *bufio.Scanner, h *Hunk, lineNum *int) error {
	oldCur, newCur := h.OldStart, h.NewStart
	oldSeen, newSeen := 0, 0

	for oldSeen < h.OldCount || newSeen < h.NewCount {
		if !sc.Scan() {
			return fmt.Errorf(
				"truncated: consumed %d/%d old and %d/%d new lines before EOF",
				oldSeen, h.OldCount, newSeen, h.NewCount,
			)
		}
		*lineNum++
		raw := sc.Text()

		// "\ No newline at end of file" annotates the previous line and counts
		// toward neither total.
		if strings.HasPrefix(raw, `\`) {
			continue
		}

		var kind LineKind
		var text string
		switch {
		case raw == "":
			// Some tools emit a bare empty line for an empty context line
			// rather than a single space. Treat it as context.
			kind, text = Context, ""
		case raw[0] == '+':
			kind, text = Added, raw[1:]
		case raw[0] == '-':
			kind, text = Deleted, raw[1:]
		case raw[0] == ' ':
			kind, text = Context, raw[1:]
		default:
			return fmt.Errorf("unexpected line prefix %q", firstRune(raw))
		}

		l := Line{Kind: kind, Text: text}
		switch kind {
		case Context:
			l.OldLine, l.NewLine = oldCur, newCur
			oldCur++
			newCur++
			oldSeen++
			newSeen++
		case Added:
			l.NewLine = newCur
			newCur++
			newSeen++
		case Deleted:
			l.OldLine = oldCur
			oldCur++
			oldSeen++
		}

		if oldSeen > h.OldCount || newSeen > h.NewCount {
			return fmt.Errorf(
				"overran header counts (old %d/%d, new %d/%d)",
				oldSeen, h.OldCount, newSeen, h.NewCount,
			)
		}
		h.Lines = append(h.Lines, l)
	}
	return nil
}

// parseHunkHeader parses "@@ -l[,s] +l[,s] @@[ section]".
func parseHunkHeader(line string) (Hunk, error) {
	var h Hunk
	end := strings.Index(line[2:], "@@")
	if end < 0 {
		return h, fmt.Errorf("malformed hunk header %q", line)
	}
	ranges := strings.Fields(line[2 : 2+end])
	h.Section = strings.TrimSpace(line[2+end+2:])
	if len(ranges) != 2 || !strings.HasPrefix(ranges[0], "-") || !strings.HasPrefix(ranges[1], "+") {
		return h, fmt.Errorf("malformed hunk ranges %q", line)
	}
	var err error
	if h.OldStart, h.OldCount, err = parseRange(ranges[0][1:]); err != nil {
		return h, fmt.Errorf("bad old range in %q: %w", line, err)
	}
	if h.NewStart, h.NewCount, err = parseRange(ranges[1][1:]); err != nil {
		return h, fmt.Errorf("bad new range in %q: %w", line, err)
	}
	return h, nil
}

// parseRange parses "start" or "start,count"; a missing count means 1.
func parseRange(s string) (start, count int, err error) {
	count = 1
	if i := strings.IndexByte(s, ','); i >= 0 {
		if count, err = strconv.Atoi(s[i+1:]); err != nil {
			return 0, 0, err
		}
		s = s[:i]
	}
	if start, err = strconv.Atoi(s); err != nil {
		return 0, 0, err
	}
	return start, count, nil
}

// parseDiffGitPaths extracts paths from `diff --git a/x b/y`. It gives up on
// paths containing spaces or quoting, because the ---/+++ headers that follow
// are authoritative and unambiguous.
func parseDiffGitPaths(line string) (old, new string, ok bool) {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		return "", "", false
	}
	return stripPathPrefix(parts[0]), stripPathPrefix(parts[1]), true
}

// stripPathPrefix removes git's a/ or b/ prefix and any surrounding quotes,
// and drops a trailing tab-separated timestamp (POSIX diff emits those).
func stripPathPrefix(p string) string {
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	p = strings.Trim(p, `"`)
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}
