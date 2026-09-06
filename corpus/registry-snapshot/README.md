# Pinned registry snapshot

Cached PyPI existence verdicts (`{name, status, exists, checked}`) as of the
benchmark run, one file per PEP 503-normalized distribution name.

**Why this is committed.** A benchmark whose result depends on a live network
call is not reproducible: PyPI is a moving target, and a reader re-running the
numbers next year would be measuring a different oracle, not a different
detector. Pinning the oracle makes `scripts/benchmark.sh` deterministic and
auditable offline, from a fresh clone, with no credentials.

**This is not the same as the corpus being valid.** Freshness is checked
separately and independently by `python3 -m bench verify-seeded`, which bypasses
every cache and re-queries PyPI live — it exits 1 if any seeded name has since
been registered. Run that before trusting a published figure; run this snapshot
to reproduce one.

Distinct from `.safecommit-cache/` (gitignored), which is the detector's
ordinary working cache during development.

Regenerate by pointing a live run at this directory:

```sh
python3 -m bench eval \
  --tool "./bin/safecommit scan --no-repo-context --cache-dir corpus/registry-snapshot" \
  --corpus corpus/known-good --corpus corpus/seeded
```
