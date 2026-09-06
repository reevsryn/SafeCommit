# Holdout protocol

Written **before** the holdout was mined or run, so the commitments below
cannot be adjusted to fit the result. Committed 2026-09-06.

## Why this exists

Every number SafeCommit has produced so far comes from `corpus/known-good`,
which is a **development set**. Requirements R1–R5 in `PHASE1-NOTES.md` were all
derived by inspecting it; a Phase 1 feasibility probe walked its unresolved
candidates; suppression rules were written against its contents. A 0-noise
result on that corpus demonstrates a working pipeline. It cannot demonstrate a
low false-positive rate, because the detector was shaped by the very data it is
being scored on.

`corpus/holdout` is mined from **different repositories**, using the same seven
gates, and is scored **once**.

## The one-shot rule

**A holdout is worth exactly one measurement.**

The following BURN it — after any of them, the corpus is a second development
set and a fresh holdout is required before publishing another figure:

- changing detector code because of a failure this corpus revealed;
- adding a name to the alias table, the stdlib list, or the path-suppression
  rules after seeing it fail here;
- re-running after a fix and reporting the improved number;
- selecting which repos or cases to keep based on how the detector scored.

The following are ALLOWED:

- `bench verify` as QA on the **mining** (catching mangled diffs and extraction
  bugs), with quarantine and adjudication, all completed **before** the detector
  is ever run;
- reading and analysing failures in order to *describe* them honestly;
- deciding, on the evidence, that a fix is warranted — provided the fix is
  reported as tuned, and the next published figure comes from a new holdout.

## What this corpus can and cannot measure

**Can:** noise / false-positive rate on real merged PRs. Every finding is a
false positive by construction.

**Cannot:** recall. There are no seeded truths here. Recall continues to come
from `corpus/seeded`, which is self-authored and unchanged — and that limitation
stands regardless of how the holdout scores.

## Prediction, recorded before the run

Stated in advance so the result can falsify it:

1. **Noise will not be zero.** The development set's zero was achieved on ~31
   distinct third-party names reaching the registry step. A fresh set of
   repositories will exercise names the alias table has never seen. I expect
   **somewhere between 1 and 10 false positives**; more than ~15 would mean the
   approach is weaker than the dev set suggested, and zero would be genuinely
   surprising.
2. **The most likely cause is R4** — import name ≠ distribution name, for
   packages absent from the ~60-entry alias table (`internal/resolve/aliases.go`).
   These show up as `registry-404` on a real, popular package.
3. **Second most likely: first-party resolution.** Repos with layouts the
   diff-path heuristic does not cover (namespace packages, unusual `src/`
   nesting) would produce `registry-404` on the project's own modules.
4. **Third: manifest scanner shapes** not present in the dev set — a
   `dependencies` array in an unexpected table, or a Poetry variant.

If false positives appear and are dominated by (2), R5's confidence gate
becomes justified by measurement rather than anticipation, which is exactly the
condition its deferral was written against.

## Repositories

Chosen for activity, review bar, and **dependency-surface diversity**, with no
overlap against the development set (aiohttp, django, httpx, pandas, black,
pip, pytest, scikit-learn). Selection was fixed before mining and is not
revisited based on results.


---

## Outcome (recorded 2026-09-06) — this holdout is now SPENT

Scored once. **7 findings across 5 of 132 cases (3.8%), two distinct names**,
both first-party resolution failures (`tests_common`, `extract_permissions`).

Against the prediction above: the count fell inside the stated 1–10 band, but
the *ranking of causes was wrong*. R4 alias gaps — predicted as the dominant
cause — produced **zero** failures across 203 registry resolutions. Cause (3),
first-party resolution, accounted for **all** of them.

Full diagnosis: `PHASE1-NOTES.md` R6.

Per the one-shot rule, any fix motivated by these failures burns this corpus.
The next published noise figure must come from a newly mined holdout, from
repositories not used here and not used in the development set.


---

# Holdout #2

Mined 2026-09-06, after the R6 fix (repo-context first-party resolution).
Recorded **before scoring**, as before.

## Why a second holdout was required

Holdout #1 is spent: its failures motivated `internal/repoindex`, so its
post-fix score (7 → 0) is tuned and establishes only that the fix addresses
what it was built for.

## What changed in how it is scored

Holdout #1 was scored in the **degraded** mode (`--no-repo-context`), because
the corpus carried no checkout. Holdout #2 is scored **with captured repo
context** (`bench context`), which is the mode production actually runs in —
the CLI runs inside a repo, and the Action runs after `actions/checkout`. This
is a fairer test of the shipped product, and a stricter one, because the fix now
has to work rather than being structurally unable to.

## Repositories

Chosen for **nested and monorepo layouts specifically**, because all seven of
holdout #1's failures came from a single such repository (Airflow) and a fix
validated against one project is not validated at all: dagster, beam, ray,
prefect, bokeh, mlflow, dbt-core, kedro, metaflow, ansible. No overlap with the
development set or holdout #1.

## Prediction, recorded before scoring

1. **Fewer than holdout #1's 7, but not zero. I expect 0–6 false positives.**
   The dominant cause is fixed; the remaining surface is thinner but real.
2. **Most likely cause: truncated trees.** GitHub truncates very large tree
   responses, and ray/ansible/beam are enormous. A truncated capture yields
   partial context, so first-party names in the missing portion resolve exactly
   as they did before the fix. `bench context` warns on truncation and the
   context file records `_truncated`; if failures cluster in a repo flagged
   that way, this is the cause.
3. **Second: namespace packages** (PEP 420) that sit at neither the repo root
   nor under a `src/` directory and carry no `__init__.py`. All three top-level
   rules miss those by construction.
4. **Third: R4 alias gaps.** Still unobserved after 203 resolutions in
   holdout #1, but this set is ML/data-tooling heavy and will exercise a
   different dependency surface.

If the count lands at zero I will say so, and also say plainly that a single
clean holdout is weaker evidence than it looks — the honest read would be
"no measured false positives on 2 sets totalling ~260 real PRs", not "zero
false-positive rate".
