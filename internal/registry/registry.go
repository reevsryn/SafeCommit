// Package registry answers "does this distribution exist on PyPI?" against
// ground truth, with an on-disk cache.
//
// # Why the result is tri-state
//
// The single most important type in this package is Status, and specifically
// that Unknown is not Absent. A network timeout, a proxy failure, an offline
// run, or a 500 from the index all mean "we do not know" — and a detector that
// treats "do not know" as "does not exist" turns every flaky network moment
// into a wave of false accusations against real packages. Only a definitive
// 404 is Absent. Everything else stays silent.
//
// # Cache boundary
//
// This cache is deliberately SEPARATE from .bench-cache/pypi used by
// bench/verify.py. corpus/README.md is explicit that the verifier's oracle
// must not be conflated with the detector's: the verifier QAs the mining,
// while this is the detector's own evidence. Sharing one cache directory would
// blur a boundary the project has already gone to some trouble to draw.
package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Status is the tri-state result of an existence check.
type Status int

const (
	Unknown Status = iota // could not determine — MUST NOT produce a finding
	Exists                // definitively present
	Absent                // definitively absent (HTTP 404)
)

func (s Status) String() string {
	switch s {
	case Exists:
		return "exists"
	case Absent:
		return "absent"
	default:
		return "unknown"
	}
}

// Result carries the verdict plus human-readable evidence for the finding.
type Result struct {
	Status   Status
	Evidence string
}

var normalizeRE = regexp.MustCompile(`[-_.]+`)

// Normalize applies PEP 503 name normalization: lowercase, and runs of
// - _ . collapse to a single -. PyPI treats Foo_Bar, foo-bar and foo.bar as
// the same project, and so must we — on both sides of every comparison.
func Normalize(name string) string {
	return strings.ToLower(normalizeRE.ReplaceAllString(strings.TrimSpace(name), "-"))
}

const userAgent = "safecommit/0.1 (+https://github.com/reevsryn/safecommit)"

// PyPI is a cached PyPI existence oracle.
type PyPI struct {
	CacheDir string
	BaseURL  string // override for tests; defaults to the real index
	Offline  bool   // never touch the network; uncached names are Unknown
	Timeout  time.Duration
	Delay    time.Duration // politeness pause after each live request

	mu     sync.Mutex
	client *http.Client
	mem    map[string]Result // in-process memo, avoids re-reading cache files
}

func NewPyPI(cacheDir string, offline bool) *PyPI {
	return &PyPI{
		CacheDir: cacheDir,
		Offline:  offline,
		BaseURL:  "https://pypi.org/pypi",
		Timeout:  20 * time.Second,
		Delay:    150 * time.Millisecond,
		client:   &http.Client{Timeout: 20 * time.Second},
		mem:      make(map[string]Result),
	}
}

type cacheEntry struct {
	Name    string `json:"name"`
	Status  int    `json:"status"` // HTTP status
	Exists  bool   `json:"exists"`
	Checked string `json:"checked"`
}

// Exists reports whether a distribution name is present on PyPI.
func (p *PyPI) Exists(name string) Result {
	norm := Normalize(name)
	if norm == "" {
		return Result{Unknown, "empty name"}
	}

	p.mu.Lock()
	if r, ok := p.mem[norm]; ok {
		p.mu.Unlock()
		return r
	}
	p.mu.Unlock()

	if r, ok := p.readCache(norm); ok {
		p.memo(norm, r)
		return r
	}
	if p.Offline {
		return Result{Unknown, "offline and not cached"}
	}

	r := p.fetch(norm)
	if r.Status != Unknown {
		p.writeCache(norm, r)
	}
	p.memo(norm, r)
	return r
}

func (p *PyPI) memo(norm string, r Result) {
	p.mu.Lock()
	p.mem[norm] = r
	p.mu.Unlock()
}

func (p *PyPI) cachePath(norm string) string {
	// Normalized names are [a-z0-9-] plus whatever survived normalization;
	// keep them filesystem-safe.
	safe := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(norm)
	return filepath.Join(p.CacheDir, safe+".json")
}

func (p *PyPI) readCache(norm string) (Result, bool) {
	b, err := os.ReadFile(p.cachePath(norm))
	if err != nil {
		return Result{}, false
	}
	var e cacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return Result{}, false
	}
	st := Absent
	if e.Exists {
		st = Exists
	}
	return Result{st, fmt.Sprintf("PyPI %d (cached %s)", e.Status, e.Checked)}, true
}

func (p *PyPI) writeCache(norm string, r Result) {
	if err := os.MkdirAll(p.CacheDir, 0o755); err != nil {
		return // cache is an optimization; never fail a scan over it
	}
	e := cacheEntry{
		Name:    norm,
		Status:  404,
		Exists:  r.Status == Exists,
		Checked: time.Now().Format("2006-01-02"),
	}
	if r.Status == Exists {
		e.Status = 200
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	tmp := p.cachePath(norm) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p.cachePath(norm))
	}
}

func (p *PyPI) fetch(norm string) Result {
	base := p.BaseURL
	if base == "" {
		base = "https://pypi.org/pypi"
	}
	url := base + "/" + norm + "/json"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Result{Unknown, "bad request: " + err.Error()}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// Network failure is NOT evidence of absence.
		return Result{Unknown, "network error: " + err.Error()}
	}
	defer resp.Body.Close()
	if p.Delay > 0 {
		time.Sleep(p.Delay)
	}

	today := time.Now().Format("2006-01-02")
	switch {
	case resp.StatusCode == http.StatusOK:
		return Result{Exists, fmt.Sprintf("PyPI 200 (checked %s)", today)}
	case resp.StatusCode == http.StatusNotFound:
		return Result{Absent, fmt.Sprintf("PyPI 404 (checked %s)", today)}
	default:
		// 429, 5xx, anything else: we do not know.
		return Result{Unknown, fmt.Sprintf("PyPI %d (checked %s)", resp.StatusCode, today)}
	}
}
