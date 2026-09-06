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

## R4 — Import name != distribution name is a standing precision risk
*(discovered 2026-08-06, while building the step 4 cascade)*

**Requirement.** `import yaml` is provided by a project called `PyYAML`; there
is no PyPI project named `yaml`. Looking up import names directly against the
registry therefore returns 404 — "hallucinated!" — for some of the most widely
used packages in Python. `internal/resolve/aliases.go` maps the letter-level
mismatches, and PEP 503 normalization already absorbs case and `-_.` variance
(`flask_cors` -> `Flask-Cors` needs no entry).

**Why this is not solved, only mitigated.** The table is finite; the real
mapping is not. PyPI exposes no import-name -> distribution-name index, so
there is no complete oracle to consult. Coverage is "mismatches common enough
to appear in real diffs" — a judgement call, not a guarantee. Every missing
entry is a latent false positive against correct code.

**Evidence it currently holds.** 0 false positives across the 96-PR known-good
corpus, with 31 candidates resolved via registry lookup (some through aliases).
That is reassuring but the sample is small: only ~31 distinct third-party names
reached the registry step at all.

**What would falsify it.** A fresh holdout (Phase 4) drawn from different repos
will exercise different third-party names. If false positives appear there,
they will most likely be alias gaps, not extraction bugs. The step 5 confidence
gate is the intended structural fix: hold back a name that is absent but shows
no hallucination signal (no typo-distance to a real package, no composition
shape), rather than firing on absence alone.

## R1 case 2 — RESOLVED (2026-09-06, step 5)

`internal/pathrules` suppresses candidates in fixture-data paths. The rule is
keyed on a data/fixture path **component** (`tests/data/`, `testdata/`,
`fixtures/`, `__snapshots__/`), never on a test root, exactly as R1's
anti-overreach constraint requires.

Verified both directions against the corpus:
- `tests/data/cases/*.py` (psf/black) -> suppressed (13 candidates across the
  known-good corpus now resolve as `fixture-data`).
- `tests/test_schemas.py` (seed-008) -> still fires. This is the tripwire R1
  asked for, and it works: suppressing `tests/` wholesale would drop it.

Note what this fixed was *latent*, not measured. Black's `from m import` only
escaped before because a PyPI project named `m` coincidentally exists.

## R3 — RESOLVED (2026-09-06, step 5)

`internal/manifest` adds the second detection path. Recall is now 18/18 on
imports and 3/3 on manifests. The two paths remain reported separately, which
is the point: they share almost no code (manifest entries are already
distribution names, so no stdlib check, no first-party derivation and no
import-name aliasing applies -- `ResolveDep` is registry-only).

**pyproject.toml is scanned, not parsed.** A diff hunk is not valid TOML, and
bench/verify.py rightly called regex-parsing TOML "itself a bug source". The
scanner is a narrow state machine answering one question -- is this added line
inside a dependency array? -- anchored on table headers and array openers taken
from the hunk's own context lines, yielding nothing when it cannot tell.

The refusals are tested against real merged PRs, because all three of these
were added by PRs in the known-good corpus and would be false positives under a
naive "any quoted string in any array" rule:

```
"testing/plugins_integration",   inside norecursedirs = [   (pytest #14540)
omit = ["venv/*"]                coverage config            (httpx  #3319)
scripts.pytest = "..."           entry point                (pytest #14126)
```

Manifest-path noise floor across the whole known-good corpus: 2 extracted names
(`ruff`, `mypy`), both real.

## R5 — Confidence gate DEFERRED to Phase 4, deliberately
*(decision 2026-09-06, owner-approved)*

Step 5 was originally scoped to include a hallucination-signal confidence gate
(fire only when an absent name also looks like a hallucination -- typo distance
to a real package, composition shape) as the structural mitigation for R4's
alias-gap risk.

**It was not built, on purpose.** There are currently zero measured false
positives. Building a gate now would mean tuning against a corpus containing no
errors to fix -- speculation, paid for in recall on novel hallucinations, which
is the exact capability the product exists to provide.

**Revisit when:** the Phase 4 fresh holdout produces real false positives. If it
does, alias gaps (R4) are the most likely cause and the gate is the intended
fix. If it does not, the gate should never be built at all. Either way the
decision will rest on measurement rather than anticipation.

## R2 — RESOLVED (2026-09-06, step 6)

`bench/score.py` now matches file-aware by default: a finding credits a truth
only when the normalized names are equal **and** the finding agrees with the
truth's file (a truth declaring no file still matches on name alone). A
right-name/wrong-place finding now scores FP + FN instead of TP, which is
exactly the distortion R2 was written to catch.

`--match name` preserves the old loose behaviour for comparison and is labelled
in the report as invalid for publication.

Two supporting changes:
- Findings that name a truth but carry **no file at all** are counted as misses
  and reported separately (`unlocated`), so "did not find it" stays
  distinguishable from "found it but could not say where".
- The reference dummy tools now emit `file`, so they remain meaningful under
  the stricter rule rather than silently scoring zero.

**Result: the detector's figures did not move.** File-aware and name-only
matching both give precision 100% / recall 100% on the development set, meaning
every one of the 21 truths is located at the correct file rather than merely
named. The distortion R2 identified was real at step 3 and had already been
cured by the step-4 alias table; this change makes the guarantee structural
instead of incidental.

## Benchmark reproducibility (step 6)

`corpus/registry-snapshot/` pins the PyPI verdicts the benchmark depends on
(45 entries). A benchmark whose result depends on a live network call is not
reproducible -- a reader re-running it later would be measuring a different
oracle, not a different detector. `scripts/benchmark.sh` therefore runs offline
against the pin, and a fresh clone reproduces the numbers exactly.

Pinning the oracle is deliberately NOT the same as claiming the corpus is still
valid. That question is answered independently and live by
`python3 -m bench verify-seeded`, which bypasses every cache.

## R6 — Holdout result: first-party resolution is the real gap, not alias coverage
*(2026-09-06, one-shot measurement under corpus/HOLDOUT-PROTOCOL.md)*

**Result.** 132 real merged PRs from ten repositories with no overlap against
the development set. **7 findings across 5 cases (3.8% of cases), reducing to
just 2 distinct names.** Every finding is a false positive by construction.

```
tests_common          6 findings, 5 cases   (apache/airflow)
extract_permissions   1 finding,  1 case    (apache/airflow)
```

**The prediction was half right, and the useful half was wrong.**

- Predicted 1-10 false positives: **held** (7).
- Predicted the dominant cause would be **R4 alias gaps** (import name !=
  distribution name): **falsified.** The holdout drove **203 registry
  resolutions** (163 import-path + 40 manifest-path) across ten unfamiliar
  dependency surfaces and produced **zero** alias failures. The ~60-entry table
  held completely.
- Predicted first-party resolution as the *second* most likely cause: this was
  in fact **100% of the failures**.

**Diagnosis.** Both names are first-party and both are invisible to the
diff-path heuristic, which takes the top path component (or the second under
`src/`):

- `tests_common` lives at `devel-common/src/tests_common`; the importing files
  sit under `providers/.../tests/...` and `tests/...`, so the derived component
  is `providers` or `tests`, never `tests_common`. Verified present in the tree.
- `extract_permissions` is a sibling module, `scripts/ci/prek/extract_permissions.py`,
  imported from `scripts/ci/prek/fab_permissions_doc.py`. Derived component is
  `scripts`. Verified present (20,885 bytes).

Both were already adjudicated into the corpus during mining QA as verifier
blind spots, with independent evidence, *before* the detector was run.

**What this justifies, and what it does not.**

- It justifies implementing **`--repo-root` first-party resolution**, which is
  the feature decision A anticipated and step 3 deferred as untestable without
  checkouts. Both failing names exist in their repositories' trees, so a
  resolver that consults the checkout suppresses both. That is a reasoned
  expectation from verified evidence, **not a measurement** -- it stays
  unmeasured until a fresh holdout scores the implemented feature.
- It does **not** justify **R5's confidence gate**. The gate was conceived to
  hold back "absent names that show no hallucination signal", aimed at alias
  gaps. The observed failures are not that: they are first-party names the
  detector never recognised as first-party. A hallucination-signal gate would
  paper over the symptom while the actual defect -- missing repo context --
  went unaddressed. R5 stays deferred, now on evidence rather than caution.

**Holdout status: SPENT.** Implementing the fix burns this corpus per the
protocol. The next published noise figure requires a newly mined holdout.

**Also confirmed:** zero `SILENT` verdicts across 554 holdout candidates, so
the resolution cascade is deciding rather than abstaining on unfamiliar repos.

## R7 — Holdout #2 result: 3 findings / 150 cases, and a new failure mode
*(2026-09-06, one-shot measurement WITH repo context — production mode)*

**Result: 3 findings across 2 of 150 cases (1.3%), 2 distinct names.** Scored
with captured repo context, which is the mode the CLI and the Action actually
run in — a stricter test than holdout #1's degraded run, because the R6 fix had
to work rather than being structurally unable to.

```
opentelemetry   2 findings   dbt-labs/dbt-core
ray_release     1 finding    ray-project/ray
```

Repositories were chosen for monorepo and nested layouts precisely because
holdout #1's failures all came from one such repo. The R6 fix held across all
of them: dagster's `dagster_test`, `dagster_k8s_test_infra` and
`docs_snippets_tests`, beam, prefect, mlflow, kedro and metaflow produced zero
first-party failures.

**Against the prediction: magnitude right, ranking wrong again.**

- 0–6 false positives: **held** (3).
- Predicted primary cause, **truncated trees**: did not occur. No repo's tree
  was truncated, including ray (11,950 entries) and beam (22,520).
- Predicted second, **PEP 420 namespace packages**: did not occur.
- Predicted third, **R4 alias gaps**: occurred, 2 of the 3 findings.
- The remaining finding is a cause I did not predict at all (below).

This is now a pattern worth naming: across two holdouts the predicted *count*
has been accurate and the predicted *ranking of causes* has been wrong both
times. Predictions are still worth recording — they keep the result honest —
but their causal ordering should not be treated as a roadmap.

### Cause 1 — `opentelemetry`: R4, anticipated and deliberately unfixed

The import `opentelemetry` is provided by the distributions
`opentelemetry-api` and `opentelemetry-sdk` (both PyPI 200); the import name
itself is 404. This was spotted during mining QA, adjudicated into the corpus
as a legitimate merged PR, and the alias table was **deliberately left
unchanged** so the false positive would count. R4 is now observed, not just
theorised: 203 registry resolutions in holdout #1 produced none, and this one
produced two.

### Cause 2 — `ray_release`: the package-root heuristic's parent check

`release/ray_release/__init__.py` exists, so `ray_release` looks like a package
root — except `release/__init__.py` **also** exists, so the parent check
classifies `ray_release` as a mere subpackage and excludes it from top-level.

In reality `release/` is on sys.path when ray's release tests run, so the
import resolves. The `release/__init__.py` file is incidental.

This is the documented cost of that parent check, arriving in practice. Without
it, every subpackage in a monorepo becomes a top-level name and a hallucination
colliding with one is silently suppressed — a missed detection, which is worse
than a false positive for this product. Dagster alone contributed 395 top-level
names *with* the check; removing it would balloon that and blunt the detector.
The trade was made deliberately and this is what it costs: roughly one false
positive per 150 PRs.

**Holdout #2 status: SPENT.** Both causes are now known, so any fix for either
burns this corpus. A third holdout is required before the next published figure.

**Not fixed here, on purpose.** The obvious repairs — add `opentelemetry` to the
alias table, relax the parent check — are each a one-liner, and each would
convert this measured result into a tuned one. They are left to an explicit
decision rather than taken reflexively.

## R7 addendum — `opentelemetry` fixed; holdout #2 is now BURNED
*(2026-09-06, owner decision: fix opentelemetry, leave ray_release alone)*

`opentelemetry` -> `opentelemetry-api` added to the alias table. Holdout #2
re-runs at **1 finding / 150 cases**, down from 3.

**That 1 is a TUNED number and must never be quoted as a measurement.** The fix
was made in response to failures this corpus revealed, which is precisely what
`corpus/HOLDOUT-PROTOCOL.md` defines as burning it. The last untainted figure
from holdout #2 is the original **3 findings / 150 cases (1.3%)**. A third
holdout is required before any new noise figure can be published.

### Generalised, but with the provenance kept separate

`opentelemetry` turned out to be an instance of a pattern rather than a one-off:
a PEP 420 **namespace root**, an import name no single distribution owns, which
404s while the packages populating it are real. Three more were added from the
same pattern -- `repoze`, `sphinxcontrib`, `jaraco` -- each derived
independently and verified against PyPI (import name 404, mapped distribution
200). Those three are **not** corpus-derived and do not affect the burn status;
only `opentelemetry` does. The distinction is recorded in the table itself so a
later reader can tell which entries came from measurement and which from
reasoning.

Candidates checked and deliberately **rejected** because their import name is
itself a registered distribution: `azure`, `zope`, `paste`, `backports`,
`mypy_extensions`. An unnecessary alias is a maintenance liability and can mask
a genuine change upstream; `TestSelfRegisteredNamesHaveNoAlias` pins this.

Dead weight removed while there: identity mappings (`psutil`, `tqdm`, `regex`,
`yattag`) and case-only aliases (`jinja2`, `markdown`, `sqlalchemy`) that PEP 503
normalization already handles, plus one bogus entry (`OpenSSL_`). Table: 60 -> 51.

### `ray_release` left unfixed, by decision

The package-root parent check stands. Relaxing it would trade a rare false
positive for silent missed detections across every monorepo subpackage --
dagster alone contributes 395 top-level names with the check in place. If this
is ever revisited, the better fix is to consult the repository's packaging
configuration (`pyproject.toml` / `setup.cfg` `packages`/`package-dir`) rather
than loosening the heuristic.
