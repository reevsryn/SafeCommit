package repoindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// buildRepo lays out a fake checkout. Paths ending in "/" are directories.
func buildRepo(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if f[len(f)-1] == '/' {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The exact shape that produced 6 of the 7 holdout false positives: a package
// under a NESTED src/ directory, imported from a completely different subtree.
func TestNestedSrcLayoutPackage(t *testing.T) {
	root := buildRepo(t,
		"devel-common/src/tests_common/__init__.py",
		"devel-common/src/tests_common/test_utils/__init__.py",
		"providers/edge3/tests/unit/edge3/worker_api/test_auth.py",
		"airflow/__init__.py",
	)
	idx, err := FromFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.IsFirstParty("tests_common") {
		t.Errorf("tests_common not recognised as first-party; TopLevel=%v", keys(idx.TopLevel))
	}
	if !idx.IsFirstParty("airflow") {
		t.Error("airflow (root package) not recognised as first-party")
	}
	if idx.IsFirstParty("requests") {
		t.Error("a name the repo does not provide must not be first-party")
	}
}

// The 7th holdout false positive: a sibling module. Python puts a script's own
// directory on sys.path, so this import resolves at runtime.
func TestSiblingModule(t *testing.T) {
	root := buildRepo(t,
		"scripts/ci/prek/extract_permissions.py",
		"scripts/ci/prek/fab_permissions_doc.py",
		"scripts/other/unrelated.py",
	)
	idx, err := FromFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.IsSibling("extract_permissions", "scripts/ci/prek/fab_permissions_doc.py") {
		t.Errorf("sibling not recognised; Siblings=%v", idx.Siblings)
	}
	// Not a sibling of a file in a different directory.
	if idx.IsSibling("extract_permissions", "scripts/other/unrelated.py") {
		t.Error("sibling visibility must not leak across directories")
	}
	// And it is not importable top-level from a sys.path root.
	if idx.IsFirstParty("extract_permissions") {
		t.Error("a module three directories deep is not top-level importable")
	}
}

func TestSkipsHeavyAndHiddenDirectories(t *testing.T) {
	root := buildRepo(t,
		".git/objects/reqursts.py",
		"node_modules/nunpy/__init__.py",
		".venv/lib/python3.13/site-packages/panddas/__init__.py",
		"realpkg/__init__.py",
	)
	idx, err := FromFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.IsFirstParty("realpkg") {
		t.Error("realpkg should be first-party")
	}
	for _, bad := range []string{"reqursts", "nunpy", "panddas"} {
		if idx.IsFirstParty(bad) {
			t.Errorf("%q came from a skipped directory and must not be indexed", bad)
		}
	}
}

func TestRootLevelModulesAndPackages(t *testing.T) {
	root := buildRepo(t, "setup.py", "conftest.py", "mypkg/__init__.py", "docs/")
	idx, err := FromFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"setup", "conftest", "mypkg", "docs"} {
		if !idx.IsFirstParty(want) {
			t.Errorf("%q should be first-party (root-level entry)", want)
		}
	}
}

// The two backends must agree. A benchmark-only code path would measure
// something production does not do.
func TestCapturedBackendMatchesFilesystemBackend(t *testing.T) {
	root := buildRepo(t,
		"devel-common/src/tests_common/__init__.py",
		"scripts/ci/prek/extract_permissions.py",
		"scripts/ci/prek/fab_permissions_doc.py",
		"airflow/__init__.py",
		"setup.py",
	)
	fsIdx, err := FromFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}

	// Serialise exactly as `bench context` will, then read it back.
	wire := struct {
		TopLevel []string            `json:"top_level"`
		Siblings map[string][]string `json:"siblings"`
	}{TopLevel: keys(fsIdx.TopLevel), Siblings: map[string][]string{}}
	for dir, names := range fsIdx.Siblings {
		wire.Siblings[dir] = keys(names)
	}
	b, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	ctx := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(ctx, b, 0o644); err != nil {
		t.Fatal(err)
	}
	capIdx, err := FromContextFile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range []string{"tests_common", "airflow", "setup", "requests", "nunpy"} {
		if fsIdx.IsFirstParty(n) != capIdx.IsFirstParty(n) {
			t.Errorf("IsFirstParty(%q): fs=%v captured=%v", n, fsIdx.IsFirstParty(n), capIdx.IsFirstParty(n))
		}
	}
	f := "scripts/ci/prek/fab_permissions_doc.py"
	for _, n := range []string{"extract_permissions", "nope"} {
		if fsIdx.IsSibling(n, f) != capIdx.IsSibling(n, f) {
			t.Errorf("IsSibling(%q): fs=%v captured=%v", n, fsIdx.IsSibling(n, f), capIdx.IsSibling(n, f))
		}
	}
}

func TestNilIndexIsSafe(t *testing.T) {
	var idx *Index
	if idx.IsFirstParty("x") || idx.IsSibling("x", "a/b.py") {
		t.Error("a nil index must answer false, not panic")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A subpackage is NOT top-level importable. Treating every package in a
// monorepo as a top-level name would silently suppress a hallucinated import
// that collides with any internal module name.
func TestSubpackagesAreNotTopLevel(t *testing.T) {
	root := buildRepo(t,
		"airflow/__init__.py",
		"airflow/models/__init__.py",
		"airflow/utils/__init__.py",
		"packages/thing/thing/__init__.py", // package root under a non-src parent
	)
	idx, err := FromFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.IsFirstParty("airflow") {
		t.Error("airflow is a package root and should be first-party")
	}
	if !idx.IsFirstParty("thing") {
		t.Error("packages/thing/thing is a package root and should be first-party")
	}
	for _, sub := range []string{"models", "utils"} {
		if idx.IsFirstParty(sub) {
			t.Errorf("%q is a SUBpackage of airflow and must not be top-level importable", sub)
		}
	}
}
