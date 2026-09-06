// Package manifest extracts distribution names from dependency manifests that
// a diff adds to: requirements*.txt and pyproject.toml.
//
// This is the second detection path. PHASE1-NOTES.md R3 requires it to be
// measured separately from import extraction, because the two exercise
// completely different code and a blended recall number can hide one of them
// being entirely broken — which is exactly what happened at step 4, where
// import recall was 18/18 while manifest recall was 0/3.
//
// # Why pyproject.toml is not parsed with a TOML library
//
// A diff gives us hunks, not files. An added line like
//
//	"matplotlib-pyplot>=1.0",
//
// is not valid TOML on its own; a real parser rejects the fragment. And
// bench/verify.py deliberately refused to regex-parse TOML, calling it "itself
// a bug source" — a caution worth honouring.
//
// So this is not a TOML parser. It is a narrow state machine answering one
// question: *is this added line inside a dependency array?* It tracks table
// headers and array openers from the hunk's own context lines, and when it
// cannot tell, it yields nothing. Silence is the default, exactly as in the
// registry oracle.
//
// The real-world lines it must refuse are not hypothetical. All three of these
// were added by merged PRs in the known-good corpus:
//
//	"testing/plugins_integration",              inside norecursedirs = [   (pytest)
//	omit = ["venv/*"]                           coverage config            (httpx)
//	scripts.pytest = "_pytest.config:..."       entry point                (pytest)
//
// Treating any quoted string in any array as a dependency would turn the first
// of those into a false positive.
package manifest

import (
	"regexp"
	"strings"

	"github.com/reevsryn/safecommit/internal/diff"
)

// A Dep is one dependency name introduced by an added manifest line.
type Dep struct {
	Name string // distribution name, as written
	File string
	Line int
	Raw  string
}

var (
	reRequirementsPath = regexp.MustCompile(`(^|/)requirements[^/]*\.txt$`)
	reRequirementsDir  = regexp.MustCompile(`(^|/)requirements/[^/]+\.txt$`)

	reTableHeader = regexp.MustCompile(`^\s*\[\[?([^\]]+)\]\]?\s*$`)
	reArrayStart  = regexp.MustCompile(`^\s*["']?([A-Za-z0-9_.\-]+)["']?\s*=\s*\[`)
	reKeyValue    = regexp.MustCompile(`^\s*["']?([A-Za-z0-9_.\-]+)["']?\s*=\s*(.+)$`)
	reQuoted      = regexp.MustCompile(`["']([^"']+)["']`)

	// PEP 508: a name starts alphanumeric and may contain . - _ internally.
	rePEP508Name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*`)
)

// IsManifest reports whether a path is a dependency manifest we understand.
func IsManifest(path string) bool {
	base := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		base = path[i+1:]
	}
	return base == "pyproject.toml" ||
		reRequirementsPath.MatchString(path) ||
		reRequirementsDir.MatchString(path)
}

// FromFile extracts dependencies introduced by the added lines of one file.
func FromFile(f *diff.File) []Dep {
	if f.NewPath == "" || f.IsBinary || !IsManifest(f.NewPath) {
		return nil
	}
	isToml := strings.HasSuffix(f.NewPath, "pyproject.toml")
	var out []Dep
	for i := range f.Hunks {
		if isToml {
			out = append(out, fromPyproject(f.NewPath, &f.Hunks[i])...)
		} else {
			out = append(out, fromRequirements(f.NewPath, &f.Hunks[i])...)
		}
	}
	return out
}

// ---------------------------------------------------------------- requirements

func fromRequirements(path string, h *diff.Hunk) []Dep {
	var out []Dep
	for _, l := range h.Lines {
		if l.Kind != diff.Added {
			continue
		}
		name, ok := requirementName(l.Text)
		if !ok {
			continue
		}
		out = append(out, Dep{Name: name, File: path, Line: l.NewLine, Raw: strings.TrimSpace(l.Text)})
	}
	return out
}

// requirementName pulls the distribution name out of one requirements.txt line.
func requirementName(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", false
	}
	// Options: -r other.txt, -e ., --index-url ..., --hash=...
	if strings.HasPrefix(s, "-") {
		return "", false
	}
	// Strip an inline comment.
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// Environment marker: "pkg ; python_version < '3.9'"
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return distName(s)
}

// ------------------------------------------------------------------ pyproject

// depArrayKeys are array keys whose contents are distribution specifiers,
// regardless of which table they appear under. `dependencies` and
// `optional-dependencies` are unambiguous enough to accept without seeing the
// table header — which matters, because a hunk often does not include it.
var depArrayKeys = map[string]bool{
	"dependencies":          true,
	"optional-dependencies": true,
}

// depTables are tables where EVERY array key holds dependency specifiers.
func isDepTable(section string) bool {
	switch {
	case section == "project.optional-dependencies",
		section == "dependency-groups",
		section == "build-system":
		return true
	}
	return false
}

// isPoetryDepTable matches Poetry's key = "constraint" style tables.
func isPoetryDepTable(section string) bool {
	if section == "tool.poetry.dependencies" || section == "tool.poetry.dev-dependencies" {
		return true
	}
	return strings.HasPrefix(section, "tool.poetry.group.") &&
		strings.HasSuffix(section, ".dependencies")
}

func fromPyproject(path string, h *diff.Hunk) []Dep {
	var out []Dep

	section := ""
	arrayKey := ""
	// Git's hunk heading is the nearest preceding line matching its pattern —
	// often the very array opener we are inside. Seed from it, then let the
	// hunk's own context lines correct us (a closing "]" clears it).
	if m := reTableHeader.FindStringSubmatch(h.Section); m != nil {
		section = m[1]
	} else if m := reArrayStart.FindStringSubmatch(h.Section); m != nil {
		arrayKey = m[1]
	}

	for _, l := range h.Lines {
		if l.Kind == diff.Deleted {
			continue // not part of the post-image
		}
		text := l.Text
		added := l.Kind == diff.Added

		if m := reTableHeader.FindStringSubmatch(text); m != nil {
			section, arrayKey = m[1], ""
			continue
		}

		if m := reArrayStart.FindStringSubmatch(text); m != nil {
			key := m[1]
			inline := strings.Contains(text, "]")
			if isDepKey(section, key) && added {
				// An inline array on an added line: dependencies = ["a", "b"]
				for _, item := range reQuoted.FindAllStringSubmatch(bracketBody(text), -1) {
					if n, ok := distName(item[1]); ok {
						out = append(out, Dep{Name: n, File: path, Line: l.NewLine, Raw: strings.TrimSpace(text)})
					}
				}
			}
			if inline {
				arrayKey = ""
			} else {
				arrayKey = key
			}
			continue
		}

		if arrayKey != "" {
			if added && isDepKey(section, arrayKey) {
				if m := reQuoted.FindStringSubmatch(text); m != nil {
					if n, ok := distName(m[1]); ok {
						out = append(out, Dep{Name: n, File: path, Line: l.NewLine, Raw: strings.TrimSpace(text)})
					}
				}
			}
			if strings.Contains(text, "]") {
				arrayKey = ""
			}
			continue
		}

		// Poetry style: requests = "^2.31" inside [tool.poetry.dependencies].
		if added && isPoetryDepTable(section) {
			if m := reKeyValue.FindStringSubmatch(text); m != nil {
				key := m[1]
				// `python = "^3.11"` is an interpreter constraint, not a package.
				if key != "python" {
					if n, ok := distName(key); ok {
						out = append(out, Dep{Name: n, File: path, Line: l.NewLine, Raw: strings.TrimSpace(text)})
					}
				}
			}
		}
	}
	return out
}

func isDepKey(section, key string) bool {
	return depArrayKeys[key] || isDepTable(section)
}

// bracketBody returns the text after the first '[' so an inline array's own
// key cannot be mistaken for one of its items.
func bracketBody(s string) string {
	if i := strings.IndexByte(s, '['); i >= 0 {
		return s[i:]
	}
	return s
}

// ----------------------------------------------------------------- shared

// distName extracts a PEP 508 distribution name from a specifier, rejecting
// anything that is a URL, a path, or a direct reference — none of those are
// registry names, and treating them as such is how a config array full of
// paths becomes a wave of false positives.
func distName(spec string) (string, bool) {
	s := strings.TrimSpace(strings.Trim(strings.TrimSpace(spec), `"'`))
	if s == "" {
		return "", false
	}
	if strings.Contains(s, "://") || strings.Contains(s, " @ ") || strings.Contains(s, "/") {
		return "", false
	}
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
		return "", false
	}
	// Extras: pkg[extra1,extra2]>=1.0 -> pkg
	name := rePEP508Name.FindString(s)
	if name == "" {
		return "", false
	}
	return name, true
}
