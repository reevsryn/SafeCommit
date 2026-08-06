# Corpora — formats, the tool contract, and the seeded-naming methodology

This directory holds the **real** evaluation corpora. They are populated in
later steps of Phase 0; the tiny synthetic stand-ins used to prove the scoring
math live in `../fixtures/` instead.

Two corpora, two jobs:

| Corpus            | Built in | Label           | Measures                                  |
|-------------------|----------|-----------------|-------------------------------------------|
| `known-good/`     | step 2   | `clean`         | **Noise.** Real merged PRs → should fire ~0 times. The headline. |
| `seeded/`         | step 3   | `hallucination` | **Recall.** Diffs with known fakes → should be caught.           |

## What makes "known-good" known-good (and what does not)

The cleanliness guarantee is **survivorship**: every case is a PR that was
(a) human-reviewed and (b) merged into a major project whose CI installs the
package and imports it. A hallucinated import or fabricated dependency cannot
survive that gauntlet — independent of anything we check afterwards.

`bench verify` is **QA on the mining, not the guarantee**. It re-derives every
added import/requirement from the mined diffs and resolves each name
(relative → stdlib → first-party → PyPI). Its job is to catch contamination
and mining/extraction bugs (mangled diffs, regex misfires, genuinely weird
entries). It must **not** be cited as proof the corpus is clean, because its
registry oracle (PyPI existence) is the same oracle the Phase 1 detector uses:
certifying the corpus with the detector's own test would make a 0-noise
benchmark result partly circular. Survivorship is independent of that oracle —
that independence is the point.

Anything `bench verify` cannot resolve moves its case to `review/` (same
corpus format), annotated with signals **independent of the PyPI oracle** —
does the name exist in that repo's tree? is it edit-distance-close to a real
package (typo shape)? does it sit in test-fixture source embedded in a string?
is it guarded by `try/except ImportError`? — so a human can judge each one:
contamination vs. verifier blind spot. Quarantined cases are excluded from
scoring until adjudicated. Nothing is silently kept; nothing is silently
dropped.

Selection criteria for mining (the seven gates: merged, ≤24 months old,
human-authored, touches Python, adds an import or dependency line, size cap,
per-repo cap) are documented in `bench/mine.py` and echoed into each corpus's
`mining-report.json` together with per-gate rejection counts.

## Corpus directory format

```
<corpus>/
    manifest.jsonl       one JSON object per line, one per case
    diffs/<id>.diff      the unified diff for each case
```

`manifest.jsonl` ignores blank lines and lines starting with `#`.

**Known-good row** (a real merged PR; no truths — any finding is a false positive):

```json
{"id": "psf__requests__pr1234", "diff": "diffs/psf__requests__pr1234.diff",
 "label": "clean",
 "source": {"repo": "psf/requests", "pr": 1234, "sha": "…", "url": "…", "merged_at": "…"}}
```

**Seeded row** (one or more known hallucinations, listed under `truth`):

```json
{"id": "seed-001", "diff": "diffs/seed-001.diff", "label": "hallucination",
 "truth": [{"name": "reqursts", "file": "svc/api.py", "kind": "import"}]}
```

**Adjudication field** (optional, on any row): when `bench verify` quarantines
a case and a human rules on it, the ruling is recorded in the manifest so the
decision travels with the corpus and future verify runs respect it instead of
re-quarantining:

```json
"adjudicated": [{"name": "pip_unexpected_module_xyz", "decision": "keep",
                 "date": "2026-06-11", "reason": "string-literal fixture; see PHASE1-NOTES.md R1"}]
```

The scorer matches a tool's findings to `truth` entries by **package name**,
normalized per PEP 503 (lowercase; runs of `-` `_` `.` collapse to `-`), so
`pandas_helpers` and `pandas-helpers` are the same. File/line are recorded but
not required to match in Phase 0.

## Tool contract (what the harness expects of any tool it grades)

- **Input:** a unified diff on **stdin**.
- **Output:** a JSON array on **stdout**; each item
  `{"name": str, "file"?: str, "line"?: int, "kind"?: str, "message"?: str}`.
  `name` is the match key. Empty output (`[]` or nothing) means "no findings".
- **Exit code** is informational; the harness grades stdout, because a real CI
  tool will exit non-zero precisely *when* it has findings.

## Seeded-hallucination naming methodology (applies in step 3)

The recall set is only credible if its fake names resemble what LLMs **actually**
hallucinate. A reviewer's fair challenge is: *"did you just test against names
you knew would fail?"* The answer must be: the names fail **because they are
realistic hallucinations that we verified are absent** — not cherry-picked junk
like `faketestpkg123`. The rules below make that defensible.

### 1. Draw from real hallucination shapes

Author names in the proportions LLMs tend to produce, across these categories:

- **Typosquats / near-misses of popular packages** — character swaps, omissions,
  doubled or dropped letters, singular↔plural variants of real distributions
  (e.g. a transposition of a well-known name). These mirror the "slopsquat"
  surface. *Caveat discovered during authoring:* `-`↔`_` swaps are **not**
  registry typosquats — PEP 503 normalization makes `scikit_learn` and
  `scikit-learn` the same PyPI name, so that pattern only matters at the
  import/module level (Phase 3 territory), never for registry-existence checks.
- **Plausible-but-nonexistent libraries** — names assembled from real tokens that
  *sound* like they should exist: `<domain>_utils`, `fast<thing>`, `py<thing>`,
  `<thing>2`, `<thing>-async`, `<thing>-client`. This is the classic "the model
  assumed a helper package exists" failure.
- **Ecosystem / install-name confusion** — importing a name that is real as a
  module but is **not** the installable distribution, or conflating two projects.
  Use sparingly and label clearly; these are subtler and easier to get wrong.

### 2. Verify non-existence at authoring time — and re-verify on demand

- Every seeded name **must** return 404 on PyPI when authored. Record the check
  and the date in the manifest (e.g. a `"verified_absent": "YYYY-MM-DD"` field).
- Prefer names **unlikely to ever be registered**, so the corpus stays valid and
  we never accidentally seed a real slopsquat target. This risk has a
  **mechanism, not a reminder**: `python3 -m bench verify-seeded` re-checks
  every seeded name fresh against PyPI (cache bypassed) and exits 1 if any got
  registered — run it before publishing any benchmark numbers, and re-author
  any case it fails.
- Authoring evidence (2026-06-11): 8 of 31 hand-drafted "plausible" candidates
  turned out to be real registered packages (`fastapy`, `panddas`,
  `requests-retry`, `httpx-retry`, `boto3-utils`, `django-async-tasks`,
  `yaml-validator`, `pandas-cleaner`) — verification at authoring time is not
  optional.

### 3. Record provenance per name

For each seeded case, note **why** the name is a realistic hallucination (which
category above, and what real package it mimics or composes from). This is what
turns "names that fail" into "documented, reproducible hallucination patterns."

Each truth entry records `category`, `origin` (`hand-authored` or
`llm-harvested`), `mimics` (the auditable lineage), and `verified_absent`.
Harvested names additionally record the model ID and the verbatim prompt/output
files (committed under `seeded/harvest/`) — these are names a real production
model actually generated when asked for working code, the strongest provenance
available given that Spracklen et al.'s name list is private.

### 4. Do not use any private master list

Spracklen et al.'s ~205k hallucinated-name list is deliberately **not public**
(to deny slopsquatters a ready-made target); only the *methodology* is. We author
our own names from the public patterns above and never claim or ingest that list.

### 5. Guard against leakage

- The seeded set is authored **independently** of the known-good set.
- Suppression/classification rules are **not** tuned by peeking at seeded names.
  If we ever tune against the corpus during development, we hold out a slice and
  report on it separately.

**Holdout decision (owner, 2026-08-06).** The known-good set has already been
inspected during development: R1/R2/R3 in `PHASE1-NOTES.md` were derived from
it, and a Phase 1 feasibility probe walked its unresolved candidates. It is
therefore a **development set**, and the 0-noise number measured on it is a
tuned number, not a held-out one. Rather than shrink an already-small 96-case
set, we **mine a fresh, never-inspected known-good holdout at Phase 4**, before
publishing any benchmark figure, using the same seven gates and different
repos/date ranges. Published precision comes from the holdout; the development
set's number may be reported alongside it but must be labelled as tuned.
