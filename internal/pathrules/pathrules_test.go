package pathrules

import "testing"

func TestFixtureDataPaths(t *testing.T) {
	suppress := []string{
		"tests/data/cases/python315.py", // psf/black — the R1 case 2 evidence
		"tests/data/simple.py",
		"testing/data/x.py",
		"src/pkg/testdata/thing.py",
		"tests/fixtures/sample.py",
		"a/b/__snapshots__/x.py",
		"test/test_data/y.py",
	}
	for _, p := range suppress {
		if !IsFixtureData(p) {
			t.Errorf("IsFixtureData(%q) = false, want true", p)
		}
	}
}

// The anti-overreach constraint. Executed test files must stay reportable;
// seed-008 depends on it.
func TestExecutedTestFilesAreNotSuppressed(t *testing.T) {
	report := []string{
		"tests/test_schemas.py", // seed-008's tripwire
		"tests/unit/test_utils.py",
		"tests/conftest.py",
		"testing/test_config.py",
		"myapp/data/loader.py", // "data" outside a test root is application code
		"data/pipeline.py",
		"setup.py",
		"requirements.txt",
	}
	for _, p := range report {
		if IsFixtureData(p) {
			t.Errorf("IsFixtureData(%q) = true, want false — over-suppression", p)
		}
	}
}
