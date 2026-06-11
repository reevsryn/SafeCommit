# SafeCommit

> An AI-aware code reviewer that fires only when ground truth proves the code
> references something that does not exist. Python target; engine (later) in Go.
> Tuned for near-zero false positives.

The full, approved build plan is in
[`project-safecommit-indexed-meteor.md`](project-safecommit-indexed-meteor.md) —
that file is the source of truth.

## Status

**Phase 0 — eval harness & corpus (in progress).**
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
- [ ] Step 3 — author ~20 seeded-hallucination diffs into `corpus/seeded/`
      (methodology: see [`corpus/README.md`](corpus/README.md)).

No detector code exists yet (that is Phase 1).

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
