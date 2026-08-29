package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalize(t *testing.T) {
	// PEP 503: these are all the same project.
	for _, in := range []string{"Foo_Bar", "foo-bar", "foo.bar", "FOO--BAR", " foo_bar "} {
		if got := Normalize(in); got != "foo-bar" {
			t.Errorf("Normalize(%q) = %q, want foo-bar", in, got)
		}
	}
}

func newTestPyPI(t *testing.T, handler http.HandlerFunc) *PyPI {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := NewPyPI(t.TempDir(), false)
	p.BaseURL = srv.URL
	p.Delay = 0
	return p
}

func TestExistsAndAbsent(t *testing.T) {
	p := newTestPyPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/requests/json" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	if got := p.Exists("requests"); got.Status != Exists {
		t.Errorf("requests: got %v, want Exists", got.Status)
	}
	if got := p.Exists("reqursts"); got.Status != Absent {
		t.Errorf("reqursts: got %v, want Absent", got.Status)
	}
}

// THE critical test. A 5xx, a 429, or a dead network must never be read as
// "this package does not exist" — that would turn an index outage into a wave
// of false accusations against real packages.
func TestServerErrorsAreUnknownNotAbsent(t *testing.T) {
	for _, code := range []int{500, 502, 503, 429, 403} {
		p := newTestPyPI(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		})
		got := p.Exists("requests")
		if got.Status != Unknown {
			t.Errorf("HTTP %d: got %v, want Unknown", code, got.Status)
		}
	}
}

func TestNetworkFailureIsUnknown(t *testing.T) {
	p := NewPyPI(t.TempDir(), false)
	p.BaseURL = "http://127.0.0.1:1" // nothing listening
	p.Delay = 0
	if got := p.Exists("requests"); got.Status != Unknown {
		t.Errorf("got %v, want Unknown on connection failure", got.Status)
	}
}

func TestOfflineWithoutCacheIsUnknown(t *testing.T) {
	p := NewPyPI(t.TempDir(), true)
	if got := p.Exists("requests"); got.Status != Unknown {
		t.Errorf("got %v, want Unknown when offline and uncached", got.Status)
	}
}

func TestCacheRoundTripAndOfflineReuse(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := NewPyPI(dir, false)
	p.BaseURL = srv.URL
	p.Delay = 0
	if got := p.Exists("nunpy"); got.Status != Absent {
		t.Fatalf("got %v, want Absent", got.Status)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	// A fresh oracle over the same dir, offline, must reuse the cached verdict.
	p2 := NewPyPI(dir, true)
	got := p2.Exists("nunpy")
	if got.Status != Absent {
		t.Errorf("offline cached: got %v, want Absent", got.Status)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want still 1 (cache should serve it)", calls)
	}
}

// Unknown must never be persisted: caching "we could not tell" would make one
// transient outage permanently poison the cache.
func TestUnknownIsNotCached(t *testing.T) {
	dir := t.TempDir()
	p := NewPyPI(dir, false)
	p.BaseURL = "http://127.0.0.1:1"
	p.Delay = 0
	p.Exists("requests")

	p2 := NewPyPI(dir, true)
	if got := p2.Exists("requests"); got.Status != Unknown {
		t.Errorf("got %v; an Unknown result must not have been written to cache", got.Status)
	}
}

func TestNormalizedCacheSharing(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p := NewPyPI(t.TempDir(), false)
	p.BaseURL = srv.URL
	p.Delay = 0
	p.Exists("Foo_Bar")
	p.Exists("foo-bar") // same project per PEP 503
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (normalized names share a cache entry)", calls)
	}
}
