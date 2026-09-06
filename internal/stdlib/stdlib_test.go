package stdlib

import "testing"

func TestKnownModules(t *testing.T) {
	for _, m := range []string{"os", "sys", "json", "asyncio", "dataclasses", "typing", "re", "__future__"} {
		if !Is(m) {
			t.Errorf("Is(%q) = false, want true", m)
		}
	}
}

// Modules removed from recent Pythons must still be suppressed: legacy code
// imports them and they are not hallucinations.
func TestHistoricalModulesStillSuppressed(t *testing.T) {
	for _, m := range []string{"distutils", "imp", "telnetlib", "asynchat", "cgi"} {
		if !Is(m) {
			t.Errorf("Is(%q) = false, want true (removed from stdlib but still real)", m)
		}
	}
}

// Regression: the list was generated on Python 3.13 and went stale when 3.14
// shipped, leaving these public modules unlisted. An unlisted stdlib module is
// looked up on PyPI, 404s, and becomes a false positive against the standard
// library itself. See internal/stdlib/gen.md.
func TestPython314ModulesAreCovered(t *testing.T) {
	for _, m := range []string{"annotationlib", "compression"} {
		if !Is(m) {
			t.Errorf("Is(%q) = false; stdlib list is stale for Python 3.14", m)
		}
	}
}

func TestNonStdlib(t *testing.T) {
	for _, m := range []string{"requests", "numpy", "django", "reqursts", "nunpy"} {
		if Is(m) {
			t.Errorf("Is(%q) = true, want false", m)
		}
	}
}

// Guard against a broken go:embed silently yielding an empty list, which would
// send the whole standard library to the registry.
func TestEmbedIsPopulated(t *testing.T) {
	if n := Count(); n < 200 {
		t.Fatalf("only %d modules embedded; expected 300+", n)
	}
}
