package resolve

import (
	"testing"

	"github.com/reevsryn/safecommit/internal/pyparse"
	"github.com/reevsryn/safecommit/internal/registry"
)

// fakeOracle lets us drive every branch, including the ones a live registry
// would rarely produce.
type fakeOracle struct {
	exists  map[string]bool
	unknown map[string]bool
	asked   []string
}

func (f *fakeOracle) Exists(name string) registry.Result {
	f.asked = append(f.asked, name)
	if f.unknown[name] {
		return registry.Result{Status: registry.Unknown, Evidence: "simulated outage"}
	}
	if f.exists[name] {
		return registry.Result{Status: registry.Exists, Evidence: "PyPI 200"}
	}
	return registry.Result{Status: registry.Absent, Evidence: "PyPI 404"}
}

func imp(top, kind string) pyparse.Import {
	return pyparse.Import{Module: top, Top: top, Kind: kind}
}

func TestCascadeSuppressesWithoutTouchingRegistry(t *testing.T) {
	f := &fakeOracle{}
	r := &Resolver{FirstParty: map[string]bool{"myapp": true}, Registry: f}

	cases := []struct {
		im     pyparse.Import
		reason string
	}{
		{pyparse.Import{Module: ".sibling", Kind: pyparse.KindRelative}, "relative"},
		{imp("os", pyparse.KindImport), "stdlib"},
		{imp("dataclasses", pyparse.KindImport), "stdlib"},
		{imp("myapp", pyparse.KindImport), "first-party"},
	}
	for _, c := range cases {
		d := r.Resolve(c.im)
		if d.Verdict != Suppress || d.Reason != c.reason {
			t.Errorf("%+v: got %v/%s, want Suppress/%s", c.im, d.Verdict, d.Reason, c.reason)
		}
	}
	if len(f.asked) != 0 {
		t.Errorf("registry was consulted %v; cheap checks must short-circuit", f.asked)
	}
}

func TestRegistryAbsentFires(t *testing.T) {
	f := &fakeOracle{exists: map[string]bool{}}
	r := &Resolver{Registry: f}
	d := r.Resolve(imp("reqursts", pyparse.KindImport))
	if d.Verdict != Fire || d.Reason != "registry-404" {
		t.Fatalf("got %v/%s, want Fire/registry-404", d.Verdict, d.Reason)
	}
}

func TestRegistryHitSuppresses(t *testing.T) {
	f := &fakeOracle{exists: map[string]bool{"requests": true}}
	r := &Resolver{Registry: f}
	if d := r.Resolve(imp("requests", pyparse.KindImport)); d.Verdict != Suppress {
		t.Fatalf("got %v, want Suppress", d.Verdict)
	}
}

// The rule the whole product rests on: undetermined is not a finding.
func TestUnknownStaysSilent(t *testing.T) {
	f := &fakeOracle{unknown: map[string]bool{"anything": true}}
	r := &Resolver{Registry: f}
	d := r.Resolve(imp("anything", pyparse.KindImport))
	if d.Verdict != Silent {
		t.Fatalf("got %v, want Silent — an undetermined lookup must never fire", d.Verdict)
	}
}

// Import name != distribution name. Without the alias table these are the
// false positives that would hit the most popular packages in Python.
func TestAliasesAreAppliedBeforeLookup(t *testing.T) {
	f := &fakeOracle{exists: map[string]bool{
		"PyYAML": true, "Pillow": true, "python-dateutil": true,
		"scikit-learn": true, "beautifulsoup4": true,
	}}
	r := &Resolver{Registry: f}
	for _, name := range []string{"yaml", "PIL", "dateutil", "sklearn", "bs4"} {
		d := r.Resolve(imp(name, pyparse.KindFromImport))
		if d.Verdict != Suppress {
			t.Errorf("%s: got %v (%s), want Suppress — alias not applied", name, d.Verdict, d.Detail)
		}
	}
}

func TestDistForReportsAliasing(t *testing.T) {
	if d, ok := DistFor("yaml"); d != "PyYAML" || !ok {
		t.Errorf("DistFor(yaml) = %q,%v want PyYAML,true", d, ok)
	}
	if d, ok := DistFor("requests"); d != "requests" || ok {
		t.Errorf("DistFor(requests) = %q,%v want requests,false", d, ok)
	}
}

func TestFirstPartyFromPaths(t *testing.T) {
	got := FirstPartyFromPaths([]string{
		"django/db/models.py",
		"src/mypkg/core.py", // src-layout: the package is the SECOND component
		"setup.py",
		"tests/test_x.py",
	})
	for _, want := range []string{"django", "mypkg", "setup", "tests"} {
		if !got[want] {
			t.Errorf("missing first-party name %q in %v", want, got)
		}
	}
	if got["src"] {
		t.Error("src/ should not itself be treated as a package name")
	}
}

func TestEmptyTopStaysSilent(t *testing.T) {
	r := &Resolver{Registry: &fakeOracle{}}
	if d := r.Resolve(pyparse.Import{Kind: pyparse.KindImport}); d.Verdict != Silent {
		t.Errorf("got %v, want Silent for an unresolvable name", d.Verdict)
	}
}

// fakeRepo implements RepoIndex for cascade tests.
type fakeRepo struct {
	top      map[string]bool
	siblings map[string]map[string]bool
}

func (f *fakeRepo) IsFirstParty(top string) bool { return f.top[top] }
func (f *fakeRepo) IsSibling(top, file string) bool {
	m, ok := f.siblings[file]
	return ok && m[top]
}

// R6: the two holdout failure modes must be suppressed before the registry is
// ever consulted.
func TestRepoIndexSuppressesBeforeRegistry(t *testing.T) {
	oracle := &fakeOracle{} // everything 404s, so any leak becomes a Fire
	r := &Resolver{
		Registry: oracle,
		Repo: &fakeRepo{
			top: map[string]bool{"tests_common": true},
			siblings: map[string]map[string]bool{
				"scripts/ci/prek/fab_permissions_doc.py": {"extract_permissions": true},
			},
		},
	}

	d := r.Resolve(pyparse.Import{
		Module: "tests_common.test_utils.config", Top: "tests_common",
		Kind: pyparse.KindFromImport, File: "providers/edge3/tests/unit/x.py",
	})
	if d.Verdict != Suppress || d.Reason != "first-party/repo" {
		t.Errorf("nested src-layout package: got %v/%s, want Suppress/first-party/repo", d.Verdict, d.Reason)
	}

	d = r.Resolve(pyparse.Import{
		Module: "extract_permissions", Top: "extract_permissions",
		Kind: pyparse.KindFromImport, File: "scripts/ci/prek/fab_permissions_doc.py",
	})
	if d.Verdict != Suppress || d.Reason != "first-party/sibling" {
		t.Errorf("sibling module: got %v/%s, want Suppress/first-party/sibling", d.Verdict, d.Reason)
	}

	if len(oracle.asked) != 0 {
		t.Errorf("registry consulted %v; repo context must short-circuit", oracle.asked)
	}
}

// The repo index must not become a blanket amnesty: a genuine hallucination in
// a repo with an index still has to fire.
func TestRepoIndexDoesNotSuppressRealHallucinations(t *testing.T) {
	r := &Resolver{
		Registry: &fakeOracle{},
		Repo: &fakeRepo{
			top:      map[string]bool{"tests_common": true},
			siblings: map[string]map[string]bool{},
		},
	}
	d := r.Resolve(pyparse.Import{
		Module: "reqursts", Top: "reqursts", Kind: pyparse.KindImport, File: "app/api.py",
	})
	if d.Verdict != Fire {
		t.Fatalf("got %v/%s, want Fire — an unknown absent name must still fire", d.Verdict, d.Reason)
	}
}

func TestNoRepoIndexIsTheDegradedButSafeMode(t *testing.T) {
	r := &Resolver{Registry: &fakeOracle{exists: map[string]bool{"requests": true}}}
	if d := r.Resolve(imp("requests", pyparse.KindImport)); d.Verdict != Suppress {
		t.Errorf("got %v, want Suppress with no repo index", d.Verdict)
	}
}
