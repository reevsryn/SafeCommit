# SafeCommit

> An AI-aware code reviewer that fires only when ground truth proves the code
> references something that does not exist. Python target; engine (later) in Go.
> Tuned for near-zero false positives.

The full, approved build plan is in
[`project-safecommit-indexed-meteor.md`](project-safecommit-indexed-meteor.md) —
that file is the source of truth.

## Status

**Phase 0 — eval harness & corpus (COMPLETE).**
We build the measuring stick *before* any detector, because the whole thesis is
"low noise," which is unprovable without it.

- [x] **Step 1 — harness core + dummy tools + fixtures + tests** (this commit).
      The precision/recall math is proven on dummy tools against a tiny
      synthetic fixture corpus.
- [x] Step 2 — mined 96 real merged Python PRs (12 each from 8 major repos,
      seven selection gates) into `corpus/known-good/`; `bench verify` QA pass
      quarantined 1 case, restored by owner adjudication (string-literal
      fixture — see `PHASE1-NOTES.md` R1). Final: **96 known-good cases**.
      Per-gate counts: `corpus/known-good/mining-report.json`; name
      resolutions: `corpus/known-good/verification-report.json`.
- [x] Step 3 — authored 20 seeded-hallucination diffs (21 fake names: 10
      typosquat, 8 composition of which 2 harvested from a real LLM with
      prompts on file, 3 ecosystem-confusion) into `corpus/seeded/`, every
      name PyPI-404-verified at authoring. Methodology + provenance rules:
      [`corpus/README.md`](corpus/README.md). Re-verify anytime:
      `python3 -m bench verify-seeded`.

**Phase 1 — the detector (COMPLETE).** Engine in Go; target language Python.

- [x] **Step 1 — engine skeleton + output contract + measured baseline.**
      `cmd/safecommit` reads a diff and reports nothing, on purpose: it
      establishes the harness boundary first, so every later step's effect on
      the benchmark is attributable to that step alone. Graded clean over all
      116 cases (0 findings, 0 contract violations).
- [x] **Step 2 — unified-diff parser with real post-image line mapping.**
      `internal/diff` parses files/hunks/lines and resolves the post-image line
      number of every added line (R2 needs this; `bench/diffscan.py` has no line
      numbers). Strict about hunk arithmetic: if the lines consumed disagree
      with the `@@` header counts it errors rather than guessing, because a
      silent mismatch misnumbers every later line in the file. Validated three
      ways — unit tests incl. the deletion/context cursor cases, a parse of all
      116 real corpus diffs (13,974 added lines, all with valid post-image
      numbers), and a differential check against `bench/diffscan.py` that found
      **0 mismatches across all 116 diffs**.
- [x] **Step 3 — tree-sitter Python parsing + import extraction.** Replaces
      regex scanning with a real parse (`internal/pyparse`), then emits *every*
      extracted import as a finding — deliberately max-noise, so raw candidate
      volume is visible before any suppression exists to mask it.
      **R1 case 1 solved on real data:** pypa/pip PR13912's
      `import pip_unexpected_module_xyz` lives under `string_content -> string`
      and produces no `import_statement` node at all, so it is never extracted.
      R1 case 2 (psf/black PR5161's test-data import) *is* still extracted, as
      expected — that one needs path-aware suppression in step 5, and a test
      pins the current behaviour so the distinction cannot silently drift.
      Benchmark: noise 380, recall 90.5% — **but see the caveat below.**
- [x] **Step 4 — resolution cascade + cached registry oracle.**
      `relative -> stdlib -> first-party -> alias -> PyPI`, cheapest first, with
      the network last and an on-disk cache. Three verdicts, not two: Suppress
      ("resolved, it's fine"), Fire ("ground truth proves absence"), and
      **Silent** ("could not determine") — Silent never becomes Fire, so an
      index outage or offline run produces no findings rather than a wave of
      false accusations. **Benchmark: 0 noise, 100% precision, 85.7% recall.**
- [x] **Step 5 — dependency-manifest parsing + fixture-path suppression.**
      Adds the second detection path (`internal/manifest`): `requirements*.txt`
      line-oriented PEP 508, and a **narrow context-anchored scanner** for
      `pyproject.toml` — not a TOML parser, since a diff hunk is not valid TOML.
      It tracks table headers and array openers from the hunk's own context
      lines and yields nothing when it cannot tell. Also adds
      `internal/pathrules` for R1 case 2 (fixture data), deliberately keyed on a
      data/fixture path *component* rather than a test root, so executed test
      files still report. **The hallucination-signal confidence gate was
      deliberately NOT built** — see below.
- [x] **Step 6 — reproducible benchmark artifact + scorer tightening.**
      `scripts/benchmark.sh` regenerates [`BENCHMARK.md`](BENCHMARK.md) from a
      full run: results, per-path recall, verdict distribution, reference-tool
      comparison and provenance (commit, versions, oracle). Runs **offline
      against `corpus/registry-snapshot`, a committed pin of PyPI verdicts**, so
      a fresh clone reproduces the numbers exactly with no network and no
      credentials. Also closes **R2**: scoring is now file-aware — a finding
      must agree with the truth's *file*, not merely its name — which
      `PHASE1-NOTES.md` required before any figure could be published.

**Phase 1 is complete.** See [`BENCHMARK.md`](BENCHMARK.md) for the current
numbers and how to reproduce them.

## Current results (step 5, development set)

```
corpus           cases  findings   TP   FP   FN  precision   recall      F1
known-good          96         0    0    0    0          —        —       —
seeded              20        22   21    0    0     100.0%   100.0%  100.0%
TOTAL              116        22   21    0    0     100.0%   100.0%  100.0%

NOISE (findings on known-good): 0   ← target: 0
```

Recall **split by detection path** (R3 — a blended number would hide this):

| path | recall |
|---|---|
| import (`internal/pyparse`) | **18/18 = 100%** |
| dependency manifest (`internal/manifest`) | **3/3 = 100%** |

Full artifact, with provenance and reproduction instructions:
[`BENCHMARK.md`](BENCHMARK.md) (regenerate with `scripts/benchmark.sh`).

### Read this number with suspicion

A perfect score on the set you developed against is evidence of a working
pipeline, **not** evidence of a good detector. Every requirement the detector
satisfies (R1–R4) was derived by inspecting this corpus, there are only 21
truths in it, and the seeded cases were authored by this project. **These
figures are not publishable.** The number that counts comes from the fresh
never-inspected holdout mined at Phase 4 — see `corpus/README.md`.

What the run does legitimately establish is that the zero is real rather than
silence. All 400 known-good candidates were suppressed for a *positive* reason,
with **no Silent verdicts at all**:

```
206 suppress  first-party      132 suppress  stdlib
 27 suppress  registry-hit      20 suppress  relative
 13 suppress  fixture-data       2 suppress  registry-hit/manifest
  0 SILENT                        0 FIRE
```

And the anti-overreach guard holds: `seed-008`'s fake import in
`tests/test_schemas.py` — an *executed* test file — still fires, while black's
`tests/data/cases/*.py` fixture files are suppressed. Suppressing `tests/`
wholesale would have silently dropped that detection.

**The prediction made before step 2 held.** It was written down in advance:
*"once extraction is parser-based, SafeCommit should emit 0 findings on all 96
known-good cases — not 'few', zero."* It does. The only surviving candidate,
`pip_unexpected_module_xyz`, sits inside a `textwrap.dedent` string literal and
never becomes a candidate under a real parser.

Reproduce either number: `--offline` against a warm cache gives byte-identical
results to a live run.

**Recall went DOWN from step 3 (90.5% -> 85.7%) because it got honest.** Step 3
was credited a true positive on `seed-019` for flagging `from dateutil import
parser` — a completely legitimate import — while the actual hallucination was a
bad pin in `requirements.txt`. The alias table now resolves `dateutil` ->
`python-dateutil` correctly, so that fake credit is gone. See `PHASE1-NOTES.md`
R2.

**These numbers come from the development set and are therefore tuned.** The
published figure must come from the fresh Phase 4 holdout — see
`corpus/README.md`.

## Building the engine

```sh
go build -o bin/safecommit ./cmd/safecommit
```

Grade it with the Phase 0 harness (`--no-repo-context` is required here: the
harness runs the binary from *this* repo, and without the flag the detector
would resolve first-party names against SafeCommit's own tree instead of the
tree each diff actually came from):

```sh
python3 -m bench eval --tool "./bin/safecommit scan --no-repo-context" --corpus corpus/known-good --corpus corpus/seeded
```

## What a "benchmark" is here

A fixed pile of input diffs with known correct answers, plus a script that
scores any tool against them. The harness never parses a diff itself — it feeds
each diff to a tool-under-test and compares that tool's findings to the labels.
Because the boundary is a subprocess speaking JSON, the same harness can grade a
dummy script today and the real Go binary later, unchanged.

## Layout

```
bench/            the eval harness (Python 3, stdlib only)
  model.py        shared types: Finding, Truth, the precision/recall/F1 math
  score.py        match findings vs. truth; aggregate counts
  corpus.py       load a corpus (diffs + manifest of labels)
  runner.py       run a tool as a subprocess; orchestrate an evaluation
  report.py       render results as a table or JSON
  mine.py         (step 2) mine merged Python PRs via the GitHub API
  __main__.py     `python3 -m bench <eval|mine>`
corpus/           the REAL corpora (populated in steps 2–3) + format/methodology docs
fixtures/         tiny SYNTHETIC corpora used to prove the math offline
tools/dummy/      reference tools (never/always/keyword fire) for validating scoring
tests/            unit tests (math) + end-to-end tests (dummies over fixtures)
```

## Usage

Everything runs from the repo root with Python 3 (no pip installs, no `gh`).

Run the tests:

```sh
python3 -m unittest discover -t . -s tests -v
```

Run the bench against a dummy tool over the fixtures:

```sh
python3 -m bench eval \
  --tool "python3 tools/dummy/fixed_list.py" \
  --corpus fixtures/known-good \
  --corpus fixtures/seeded
```

Swap `fixed_list.py` for `never_fire.py` (0 noise, 0 recall) or `always_fire.py`
(max recall, heavy noise) to see the contrast. Add `--json` for machine output.

Mine and QA-verify the real known-good corpus (step 2; needs `GITHUB_TOKEN`,
e.g. in a gitignored `.env`):

```sh
python3 -m bench mine --repo django/django --repo pandas-dev/pandas --count 12
python3 -m bench verify --corpus corpus/known-good
```

`verify` resolves every added import/requirement (stdlib → first-party → PyPI)
and quarantines anything unresolved to `corpus/review/` for human judgment.
It QAs the mining; it is **not** the cleanliness guarantee — see
[`corpus/README.md`](corpus/README.md) for why (oracle circularity).

Re-check that every seeded hallucination name is still absent from PyPI
(run before publishing any benchmark numbers; exits 1 if a name got registered):

```sh
python3 -m bench verify-seeded
```

**macOS note:** with a python.org-installed Python, urllib may fail SSL
verification (`CERTIFICATE_VERIFY_FAILED`). Point it at the system CA bundle:
`export SSL_CERT_FILE=/etc/ssl/cert.pem`. No package installs needed.

## Integrity rules (followed in this repo)

- **No benchmark numbers are hardcoded.** Every metric is computed live from a
  corpus. Claims cited in the plan are to be verified, not baked into code/docs.
- **Regenerate our own hallucination set.** We author seeded names from public
  patterns; we do not use any private master list. See `corpus/README.md`.
- **Reproducible.** The corpora and the scoring script are committed so anyone
  can rerun the numbers.
