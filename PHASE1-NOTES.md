# Phase 1 notes — detector requirements discovered during Phase 0

A standing file for design requirements that the corpus and benchmark surface
*before* detector work starts. Each entry carries the real case IDs that
motivated it, so the requirement stays falsifiable and doesn't decay into
folklore. Add new entries as the corpus teaches us more.

## R1 — Context-aware suppression: string literals and test-data fixtures
*(discovered 2026-06-11, while QA-verifying the known-good corpus)*

**Requirement.** The Phase 1 classifier (pipeline stage [4]) must not treat
every syntactic import in a changed `.py` file as a candidate. Two contexts
contain imports that are syntactically real but never executed, and both occur
in top-tier real-world repos:

1. **Imports inside string literals** — test fixtures that embed Python source
   as a string (`textwrap.dedent("""...""")`, pytester-style source blocks)
   which the test then writes to disk or feeds to a tool. tree-sitter handles
   this naturally — a string literal is not an `import_statement` node — but
   only if we parse the real file contents; any line/regex-based shortcut
   re-introduces the false positive.
2. **Imports in test-data / fixture files** — whole `.py` files that exist as
   *input data* for a tool (formatter test cases, parser corpora). These parse
   as genuine import statements even under tree-sitter, so AST awareness is
   NOT sufficient; the classifier needs **path-aware suppression** (e.g.
   `tests/data/`, `test_data/`, `fixtures/`, project-specific corpora dirs)
   and/or repo-config allowlisting.

**Evidence (both from the mined known-good corpus):**

- `pypa__pip__pr13912` — `import pip_unexpected_module_xyz` inside a
  `textwrap.dedent` string in `tests/functional/test_install.py`. The module's
  nonexistence is the test's point. Our regex-based corpus verifier (correctly
  humble about this) quarantined it; the owner adjudicated it back in as a
  deliberately-hard noise case. A line-scanning detector false-positives here;
  a parsing detector must not.
- `psf__black__pr5161` — `from m import (...)` in a black formatter test-data
  file. Syntactically a real import even to a parser. It evaded our QA only
  because a PyPI distribution named `m` coincidentally exists — i.e. this
  pattern WILL false-positive on registry lookup the moment a fixture uses a
  placeholder name that isn't coincidentally registered.

**Anti-overreach constraint.** Do **not** suppress all of `tests/`. Imports at
the top of *executed* test files run under pytest and a hallucinated one fails
CI — those are legitimate findings. The boundary is "code that executes" vs.
"code that is data". The step 3 seeded corpus should include a fake import in
an executed test file as a tripwire against overbroad suppression.

**Benchmark implication.** Keep both cases in the known-good set. They are the
noise floor doing its job: they punish naive line-scanning tools and reward the
parse-then-verify architecture — which is the product's whole bet.

## R2 — File/line-aware finding↔truth matching (scoring refinement, pre-publication)
*(discovered 2026-06-11, while authoring the seeded corpus)*

**Requirement.** Phase 0 scoring matches findings to truths by **package name
only** (PEP 503-normalized; a deliberate simplification documented in
`bench/score.py`). That granularity cannot tell "flagged the right line" from
"flagged the wrong line." Before publishing final benchmark numbers, tighten
`match_case` to require file agreement (and line where available) between a
finding and the truth it claims credit for. The plumbing already exists on both
sides: truth entries record `file`/`kind`, and the tool contract carries
`file`/`line` per finding.

**Evidence.** `seed-019`: the code import `from dateutil import parser` is
*legitimate* (the module exists once `python-dateutil` is installed); only the
`dateutil>=2.9` pin in `requirements.txt` is the hallucination. Under
name-only matching, a tool that wrongly flags the legitimate import line still
scores a TP for the truth it didn't actually find.

## R3 — Report recall per detection path: imports vs. dependency manifests
*(discovered 2026-06-11, from the three-dummy check on the real corpora)*

**Requirement.** Code-import truths and dependency-manifest truths exercise
**different detector code paths** (AST/import extraction vs. manifest parsing).
`bench` must break recall out by truth `kind` (`import` vs. `requirement`)
whenever it reports, so a blended number cannot hide a detector that is strong
on one path and broken on the other. The same breakdown applies to SafeCommit
itself in Phase 1: diff→import extraction and `requirements.txt`/`pyproject.toml`
parsing each get their own recall line.

**Evidence.** `always_fire` (an import-line-only dummy) reports 90.5% blended
recall while structurally catching **0 of 2** manifest-only truths
(`python-requests`, `matplotlib-pyplot`; it reached `dateutil` only via that
case's coincidental legitimate import — the R2 problem compounding the R3 one).

## R2 — CONFIRMED LIVE against the real detector (2026-08-06, step 3)

R2 was predicted from the corpus; step 3's max-noise extractor demonstrates it
for real. Running the actual binary over `corpus/seeded`:

```
CREDITED A TP ON THE WRONG FILE:
  seed-019: truth 'dateutil' is in requirements.txt,
            but we flagged reports/weekly.py:2
```

`reports/weekly.py:2` is `from dateutil import parser` — a **legitimate**
import (the module exists once `python-dateutil` is installed). The
hallucination is the `dateutil>=2.9` line in `requirements.txt`, which the
step-3 detector cannot even see (it has no manifest path yet). Under name-only
matching the scorer credits a true positive anyway.

**Consequence for reporting:** step 3's headline recall of 90.5% (19/21) is
inflated. The honest figure for truths we actually *located* is 18/21 (85.7%);
one of the 19 "hits" is credit for flagging an unrelated legitimate line. Until
`match_case` requires file agreement, **any recall number this project quotes
must carry this caveat.** Tightening the scorer is a prerequisite for
publication, not a nice-to-have.

## R3 — CONFIRMED LIVE, same run

The two false negatives are exactly the manifest-only truths R3 predicted:

```
  seed-018: python-requests   (requirements.txt)
  seed-020: matplotlib-pyplot (pyproject.toml)
```

Both are invisible to the import-extraction path by construction. `dateutil`,
the third `requirement`-kind truth, was "caught" only via the R2 accident
above. So the import path's true recall on manifest truths is **0 of 3** — the
blended number hides a completely unimplemented detection path, which is
precisely what R3 exists to prevent.
