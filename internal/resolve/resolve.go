// Package resolve decides what an extracted import actually refers to.
//
// The cascade runs cheapest-and-most-certain first, and the registry — the
// only step that touches the network — runs last:
//
//	relative      -> first-party by construction, suppress
//	stdlib        -> suppress   (36% of real candidates)
//	first-party   -> suppress   (49% of real candidates)
//	alias lookup  -> rewrite import name to distribution name
//	registry 200  -> suppress
//	registry 404  -> FIRE
//	anything else -> stay silent
//
// # The rule that makes the whole product work
//
// There are three verdicts, not two. Suppress means "we resolved it and it is
// fine". Fire means "ground truth proves it does not exist". Silent means "we
// could not tell" — and Silent must never become Fire. Every ambiguity in this
// package resolves toward saying nothing, because a tool that cries wolf on a
// flaky network or an unusual-but-real package is exactly the noisy scanner
// SafeCommit exists to not be.
package resolve

import (
	"strings"

	"github.com/reevsryn/safecommit/internal/pyparse"
	"github.com/reevsryn/safecommit/internal/registry"
	"github.com/reevsryn/safecommit/internal/stdlib"
)

type Verdict int

const (
	Silent   Verdict = iota // undetermined — emit nothing
	Suppress                // resolved as legitimate
	Fire                    // proven absent from the registry
)

// A Decision explains itself. The reason string ends up in the PR comment, and
// "we could not verify X" must be distinguishable from "X does not exist".
type Decision struct {
	Verdict Verdict
	Reason  string // short machine-ish label: stdlib, first-party, registry-404
	Detail  string // human evidence: "PyPI 404 (checked 2026-08-06)"
	Dist    string // the distribution name actually looked up
}

// An Oracle answers existence questions. It is an interface so the cascade can
// be tested exhaustively — especially its Unknown paths — without a network.
type Oracle interface {
	Exists(name string) registry.Result
}

// A Resolver resolves imports for ONE diff. FirstParty is derived per-diff.
type Resolver struct {
	FirstParty map[string]bool
	Registry   Oracle
}

// Resolve runs the cascade for a single import.
func (r *Resolver) Resolve(im pyparse.Import) Decision {
	if im.Kind == pyparse.KindRelative {
		return Decision{Suppress, "relative", "relative import: first-party by construction", ""}
	}
	top := im.Top
	if top == "" {
		return Decision{Silent, "no-name", "no resolvable top-level name", ""}
	}
	if stdlib.Is(top) {
		return Decision{Suppress, "stdlib", "Python standard library", ""}
	}
	if r.FirstParty[top] {
		return Decision{Suppress, "first-party", "matches a path in this diff", ""}
	}

	dist, aliased := DistFor(top)
	res := r.Registry.Exists(dist)
	detail := res.Evidence
	if aliased {
		detail = "import name maps to distribution " + dist + "; " + detail
	}

	switch res.Status {
	case registry.Exists:
		return Decision{Suppress, "registry-hit", detail, dist}
	case registry.Absent:
		return Decision{Fire, "registry-404", detail, dist}
	default:
		return Decision{Silent, "unverified", detail, dist}
	}
}

// FirstPartyFromPaths derives the top-level module names this repo defines,
// using only the paths the diff itself touches.
//
// This is what makes the benchmark honest without a checkout: a diff that adds
// django/db/models.py tells us `django` is first-party. Measured against a
// GitHub-API-backed resolution over the 96-PR corpus, this recovers 209 of 220
// first-party names; the 11 it misses (self-imports from tests/ and examples/
// directories) all exist on PyPI and are suppressed one step later, so they
// cost a network lookup rather than a false positive.
func FirstPartyFromPaths(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		if t := topComponent(p); t != "" {
			out[t] = true
		}
	}
	return out
}

// topComponent mirrors bench/verify.py's _top_component so the Go detector and
// the Python corpus QA agree on what "first-party" means.
func topComponent(path string) string {
	parts := strings.Split(path, "/")
	top := parts[0]
	if top == "src" && len(parts) > 1 {
		top = parts[1]
	}
	return strings.TrimSuffix(top, ".py")
}
