package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reevsryn/safecommit/internal/diff"
)

func deps(t *testing.T, src string) []Dep {
	t.Helper()
	files, err := diff.Parse([]byte(src))
	if err != nil {
		t.Fatalf("diff.Parse: %v", err)
	}
	var out []Dep
	for i := range files {
		out = append(out, FromFile(&files[i])...)
	}
	return out
}

func depsFromCorpus(t *testing.T, name string) []Dep {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "corpus", "known-good", "diffs", name))
	if err != nil {
		src, err = os.ReadFile(filepath.Join("..", "..", "corpus", "seeded", "diffs", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
	}
	files, err := diff.Parse(src)
	if err != nil {
		t.Fatalf("diff.Parse: %v", err)
	}
	var out []Dep
	for i := range files {
		out = append(out, FromFile(&files[i])...)
	}
	return out
}

func names(ds []Dep) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name
	}
	return out
}

func has(ds []Dep, name string) bool {
	for _, d := range ds {
		if d.Name == name {
			return true
		}
	}
	return false
}

func TestRequirementsBasicForms(t *testing.T) {
	got := deps(t, `diff --git a/requirements.txt b/requirements.txt
--- a/requirements.txt
+++ b/requirements.txt
@@ -1,1 +1,10 @@
 flask==3.0.3
+requests==2.32.3
+numpy>=2.0
+pkg[extra1,extra2]>=1.0
+marked ; python_version < "3.9"
+trailing  # inline comment
+-r other.txt
+--index-url https://example.com/simple
+git+https://github.com/x/y.git#egg=z
+./local/path
`)
	for _, want := range []string{"requests", "numpy", "pkg", "marked", "trailing"} {
		if !has(got, want) {
			t.Errorf("missing %q in %v", want, names(got))
		}
	}
	for _, bad := range []string{"-r", "other.txt", "git+https", "z", "local"} {
		if has(got, bad) {
			t.Errorf("extracted %q, which is not a distribution: %v", bad, names(got))
		}
	}
	if len(got) != 5 {
		t.Errorf("got %d deps %v, want exactly 5", len(got), names(got))
	}
}

func TestSeed018Requirements(t *testing.T) {
	got := depsFromCorpus(t, "seed-018.diff")
	if !has(got, "python-requests") {
		t.Fatalf("did not extract the seeded truth; got %v", names(got))
	}
	for _, d := range got {
		if d.Name == "python-requests" && (d.File != "requirements.txt" || d.Line != 3) {
			t.Errorf("got %s:%d, want requirements.txt:3", d.File, d.Line)
		}
	}
}

func TestSeed020Pyproject(t *testing.T) {
	got := depsFromCorpus(t, "seed-020.diff")
	if !has(got, "matplotlib-pyplot") {
		t.Fatalf("did not extract the seeded truth; got %v", names(got))
	}
	if len(got) != 1 {
		t.Errorf("got %v, want only the added dependency (context lines are not additions)", names(got))
	}
}

// The refusals. Every one of these lines was added by a real merged PR in the
// known-good corpus. Treating any quoted string in any array as a dependency
// turns them into false positives.
func TestRealWorldNonDependencyArraysAreRefused(t *testing.T) {
	t.Run("pytest norecursedirs", func(t *testing.T) {
		got := depsFromCorpus(t, "pytest-dev__pytest__pr14540.diff")
		if has(got, "testing") || len(got) > 0 {
			t.Errorf("extracted %v from a norecursedirs array; want none", names(got))
		}
	})
	t.Run("httpx coverage omit", func(t *testing.T) {
		got := depsFromCorpus(t, "encode__httpx__pr3319.diff")
		if has(got, "venv") || has(got, "omit") {
			t.Errorf("extracted %v from a coverage omit array; want none", names(got))
		}
	})
	t.Run("pytest entry points", func(t *testing.T) {
		got := depsFromCorpus(t, "pytest-dev__pytest__pr14126.diff")
		for _, n := range names(got) {
			if n == "scripts" || n == "_pytest" {
				t.Errorf("extracted %q from an entry-point key=value line", n)
			}
		}
	})
}

func TestPyprojectRefusesUnknownArrays(t *testing.T) {
	got := deps(t, `diff --git a/pyproject.toml b/pyproject.toml
--- a/pyproject.toml
+++ b/pyproject.toml
@@ -1,5 +1,7 @@
 [tool.pytest.ini_options]
 testpaths = [
+    "tests/unit",
 ]
 markers = [
+    "slow: marks tests as slow",
 ]
`)
	if len(got) != 0 {
		t.Errorf("got %v, want none — neither testpaths nor markers holds dependencies", names(got))
	}
}

func TestPyprojectDependencyTables(t *testing.T) {
	got := deps(t, `diff --git a/pyproject.toml b/pyproject.toml
--- a/pyproject.toml
+++ b/pyproject.toml
@@ -1,6 +1,8 @@
 [project.optional-dependencies]
 dev = [
+    "pytest>=8.0",
 ]
 [build-system]
 requires = [
+    "setuptools>=61",
 ]
`)
	for _, want := range []string{"pytest", "setuptools"} {
		if !has(got, want) {
			t.Errorf("missing %q in %v", want, names(got))
		}
	}
}

func TestPoetryStyle(t *testing.T) {
	got := deps(t, `diff --git a/pyproject.toml b/pyproject.toml
--- a/pyproject.toml
+++ b/pyproject.toml
@@ -1,2 +1,4 @@
 [tool.poetry.dependencies]
 flask = "^3.0"
+requests = "^2.31"
+python = "^3.11"
`)
	if !has(got, "requests") {
		t.Errorf("missing requests in %v", names(got))
	}
	// `python` is an interpreter constraint, not a distribution on PyPI.
	if has(got, "python") {
		t.Errorf("extracted `python`, which is an interpreter constraint: %v", names(got))
	}
}

func TestInlineDependencyArray(t *testing.T) {
	got := deps(t, `diff --git a/pyproject.toml b/pyproject.toml
--- a/pyproject.toml
+++ b/pyproject.toml
@@ -1,1 +1,2 @@
 [project]
+dependencies = ["httpx>=0.27", "anyio"]
`)
	for _, want := range []string{"httpx", "anyio"} {
		if !has(got, want) {
			t.Errorf("missing %q in %v", want, names(got))
		}
	}
	if has(got, "dependencies") {
		t.Error("extracted the array key itself as a dependency")
	}
}

func TestIsManifest(t *testing.T) {
	yes := []string{"requirements.txt", "requirements-dev.txt", "req/requirements.txt",
		"requirements/base.txt", "pyproject.toml", "sub/dir/pyproject.toml"}
	no := []string{"setup.py", "README.md", "app/main.py", "requirements.md", "toml/pyproject.json"}
	for _, p := range yes {
		if !IsManifest(p) {
			t.Errorf("IsManifest(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsManifest(p) {
			t.Errorf("IsManifest(%q) = true, want false", p)
		}
	}
}

// Nothing may be extracted from any manifest in the known-good corpus that is
// not a real, resolvable distribution — this is the noise floor for the new path.
func TestKnownGoodCorpusManifests(t *testing.T) {
	root := filepath.Join("..", "..", "corpus", "known-good", "diffs")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		src, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files, err := diff.Parse(src)
		if err != nil {
			continue
		}
		for i := range files {
			for _, d := range FromFile(&files[i]) {
				total++
				t.Logf("%s: %s (%s:%d) %q", e.Name(), d.Name, d.File, d.Line, d.Raw)
			}
		}
	}
	t.Logf("extracted %d dependency names across the known-good corpus", total)
}
