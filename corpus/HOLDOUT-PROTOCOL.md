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
