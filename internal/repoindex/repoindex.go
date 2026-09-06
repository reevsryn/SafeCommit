// Package repoindex answers "does THIS repository provide a module by this
// name?" — the first-party question the diff-path heuristic cannot.
//
// # Why this exists (PHASE1-NOTES.md R6)
//
// The holdout produced 7 false positives, and every one was a first-party name
// invisible to the diff-path heuristic, which takes a path's top component:
//
//	devel-common/src/tests_common      imported from providers/.../tests/x.py
//	scripts/ci/prek/extract_permissions.py  imported from a sibling in that dir
//
// Neither name appears as a top path component anywhere in its diff, so both
// went to PyPI, 404'd, and were reported as hallucinations. R4 alias gaps —
// the failure mode predicted as dominant — produced zero errors across 203
// registry resolutions. Repo context is the actual gap.
//
// # Two backends, one resolution rule
//
// Production has a checkout, so the index is built by walking it. The benchmark
// corpora are diffs with no checkout, so an equivalent index is built from a
// captured listing committed alongside the corpus. Both produce the same
// struct and run the same matching logic; only the source of the listing
// differs. That matters: a benchmark-only code path would measure something
// production does not do.
//
// # Bias
//
// Generous, deliberately, and for the same reason as internal/stdlib: a name
// wrongly treated as first-party costs recall on a hallucination that happens
// to collide with a local module name — vanishingly rare. A first-party name
// we fail to recognise costs a false positive against correct code, which is
// the failure this project exists to avoid. When in doubt, suppress.
package repoindex

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// skipDirs are never worth walking and can be enormous.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".tox": true, ".nox": true,
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
	".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true,
	"build": true, "dist": true, ".eggs": true, "site-packages": true,
}

const maxDepth = 5 // deep enough for nested src-layouts; bounded for huge monorepos

// An Index holds the importable names a repository provides.
type Index struct {
	// TopLevel: names importable from a sys.path root — repo-root entries and
	// the children of any src/ directory at any depth.
	TopLevel map[string]bool `json:"top_level"`
	// Siblings: directory -> the module names it contains. Python puts a
	// script's own directory on sys.path, so `from extract_permissions import x`
	// resolves for any file in the same directory.
	Siblings map[string]map[string]bool `json:"siblings"`
}

func hasInit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "__init__.py"))
	return err == nil
}

func newIndex() *Index {
	return &Index{TopLevel: map[string]bool{}, Siblings: map[string]map[string]bool{}}
}

// IsFirstParty reports whether the repository provides this top-level name.
func (i *Index) IsFirstParty(top string) bool {
	if i == nil || top == "" {
		return false
	}
	return i.TopLevel[top]
}

// IsSibling reports whether `top` is a module sitting beside importingFile.
func (i *Index) IsSibling(top, importingFile string) bool {
	if i == nil || top == "" || importingFile == "" {
		return false
	}
	dir := path.Dir(importingFile)
	if dir == "." {
		dir = ""
	}
	names, ok := i.Siblings[dir]
	return ok && names[top]
}

// moduleName returns the importable name an entry contributes, if any.
func moduleName(name string, isDir bool) (string, bool) {
	if name == "" || strings.HasPrefix(name, ".") || skipDirs[name] {
		return "", false
	}
	if isDir {
		return name, true
	}
	if strings.HasSuffix(name, ".py") {
		return strings.TrimSuffix(name, ".py"), true
	}
	if strings.HasSuffix(name, ".pyi") {
		return strings.TrimSuffix(name, ".pyi"), true
	}
	return "", false
}

// FromFilesystem builds an index by walking a checkout. This is the production
// path: the CLI and the GitHub Action both run inside the repository.
func FromFilesystem(root string) (*Index, error) {
	idx := newIndex()
	rootClean := filepath.Clean(root)

	err := filepath.WalkDir(rootClean, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corner of the tree: skip, never fail a scan
		}
		rel, rerr := filepath.Rel(rootClean, p)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relSlash := filepath.ToSlash(rel)
		parent := path.Dir(relSlash)
		if parent == "." {
			parent = ""
		}

		if name, ok := moduleName(d.Name(), d.IsDir()); ok {
			// Sibling visibility: every module is importable from its own directory.
			if idx.Siblings[parent] == nil {
				idx.Siblings[parent] = map[string]bool{}
			}
			idx.Siblings[parent][name] = true

			// Top-level visibility: the two standard sys.path roots.
			//   1. the repository root itself
			//   2. the children of any src/ directory, at any depth — this is
			//      the nested src-layout that produced 6 of the 7 holdout
			//      false positives (devel-common/src/tests_common)
			if parent == "" || path.Base(parent) == "src" {
				idx.TopLevel[name] = true
			}
			// 3. a PACKAGE ROOT: a directory with __init__.py whose parent has
			//    none. That is the top of a package tree, so it is importable
			//    once its parent is on sys.path.
			//
			//    The parent check is what keeps this from being absurd. Without
			//    it, every subpackage in a monorepo (utils, models, config...)
			//    would be treated as an importable top-level name, and a
			//    hallucinated import colliding with any of them would be
			//    silently suppressed. That trades a false positive for a missed
			//    detection, which is not a trade worth making blindly.
			if d.IsDir() && hasInit(p) && !hasInit(filepath.Dir(p)) {
				idx.TopLevel[name] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// FromContextFile builds an index from a captured listing. This is the
// benchmark path: corpora are diffs, so the listing a checkout would provide is
// captured once (see `bench context`) and committed with the corpus.
func FromContextFile(p string) (*Index, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var wire struct {
		TopLevel []string            `json:"top_level"`
		Siblings map[string][]string `json:"siblings"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		return nil, err
	}
	idx := newIndex()
	for _, n := range wire.TopLevel {
		idx.TopLevel[n] = true
	}
	for dir, names := range wire.Siblings {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		idx.Siblings[dir] = m
	}
	return idx, nil
}
