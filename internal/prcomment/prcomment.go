// Package prcomment renders findings as a pull-request comment.
//
// This is the product surface: for most users it is the only part of
// SafeCommit they will ever see. Two things govern the design.
//
// # Brevity is the feature
//
// The competitors this project is differentiated against are criticised for
// being "talkative" and "nitpicky" -- reviewers stop reading them. A finding
// here is one row. There is no summary of what was scanned, no praise, no
// suggestions, no restating of the diff. If SafeCommit has nothing to say it
// says nothing at all, and the caller posts no comment.
//
// # Show the evidence, not a verdict
//
// Every finding carries the ground truth that produced it -- "PyPI 404
// (checked 2026-09-06)" -- because that is the whole differentiator. An
// LLM-judge reviewer asserts; this one can be checked. A reader who doubts a
// finding can open the URL and settle it in a second.
package prcomment

import (
	"fmt"
	"sort"
	"strings"

	"github.com/reevsryn/safecommit/internal/finding"
)

// Marker identifies SafeCommit's own comment so re-runs update it in place
// instead of posting a new one on every push. Invisible in rendered markdown.
const Marker = "<!-- safecommit:findings -->"

// Options control rendering details the engine cannot know.
type Options struct {
	// LinkBase, when set, turns file references into links. Typically
	// "https://github.com/OWNER/REPO/blob/SHA/". Empty renders plain text.
	LinkBase string
	// Version of the tool, shown in the footer for reproducibility.
	Version string
}

// Render builds the comment body. It returns ok=false when there is nothing to
// say, which the caller must treat as "post no comment" rather than "post an
// empty one".
func Render(findings []finding.Finding, opts Options) (body string, ok bool) {
	if len(findings) == 0 {
		return "", false
	}

	sorted := make([]finding.Finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		return sorted[i].Line < sorted[j].Line
	})

	var b strings.Builder
	b.WriteString(Marker)
	b.WriteString("\n### SafeCommit: ")
	if len(sorted) == 1 {
		b.WriteString("1 reference that does not exist\n\n")
	} else {
		fmt.Fprintf(&b, "%d references that do not exist\n\n", len(sorted))
	}

	b.WriteString("| | where | evidence |\n|---|---|---|\n")
	for _, f := range sorted {
		b.WriteString("| `")
		b.WriteString(f.Name)
		b.WriteString("` | ")
		b.WriteString(location(f, opts.LinkBase))
		b.WriteString(" | ")
		b.WriteString(evidence(f))
		b.WriteString(" |\n")
	}

	b.WriteString("\nThese are not style opinions. Each name above was looked up in the " +
		"package registry and is absent — the signature of an AI-generated dependency " +
		"that was never real.\n")
	b.WriteString("\n<sub>Nothing left this runner except the package names above, " +
		"sent to pypi.org. Your source code was not uploaded anywhere.")
	if opts.Version != "" {
		b.WriteString(" · safecommit " + opts.Version)
	}
	b.WriteString("</sub>\n")
	return b.String(), true
}

func location(f finding.Finding, linkBase string) string {
	if f.File == "" {
		return "—"
	}
	label := f.File
	if f.Line > 0 {
		label = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	if linkBase == "" {
		return "`" + label + "`"
	}
	url := strings.TrimSuffix(linkBase, "/") + "/" + f.File
	if f.Line > 0 {
		url = fmt.Sprintf("%s#L%d", url, f.Line)
	}
	return fmt.Sprintf("[`%s`](%s)", label, url)
}

// evidence extracts the parenthesised ground truth the engine recorded, so the
// comment shows "PyPI 404 (checked ...)" rather than the full sentence.
func evidence(f finding.Finding) string {
	msg := f.Message
	// The engine's evidence is itself parenthesised — "PyPI 404 (checked ...)" —
	// so the group nests. Scanning for the LAST "(" finds the inner "(checked
	// ...)" and loses the status code, so match the balanced group instead.
	if inner, found := balancedGroup(msg, "(PyPI"); found {
		kind := "no distribution provides this import"
		if f.Kind == finding.KindRequirement {
			kind = "no such distribution"
		}
		return kind + " — " + inner
	}
	if msg == "" {
		return "absent from the package registry"
	}
	return msg
}

// balancedGroup returns the contents of the parenthesised group beginning at
// the first occurrence of `open`, respecting nesting.
func balancedGroup(s, open string) (string, bool) {
	start := strings.Index(s, open)
	if start < 0 {
		return "", false
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start+1 : i], true
			}
		}
	}
	return "", false
}
