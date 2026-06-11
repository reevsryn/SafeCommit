"""Mine real, already-merged Python PRs into a known-good ("noise") corpus.

STEP 2 of Phase 0. Approved selection criteria (2026-06-10):

  Repo pool: 8 actively developed, high-review-bar, primarily-Python projects
  (passed via --repo). Per-PR gates, in filter order -- metadata gates run
  BEFORE the diff download (each diff fetch costs a request), content gates
  after:

    1. merged (merged_at non-null)       survivorship: human review + full CI
    2. merged within the last 24 months  recency floor; also neutralizes the
                                         sort=updated ordering quirk
    3. human-authored (no bot accounts)  bot PRs are templated near-duplicates
    4. touches >=1 .py file in the post-image
    5. adds >=1 import line in a .py file, or >=1 requirement-like line in a
       dependency manifest -- every kept case must be CAPABLE of producing a
       false positive, or it adds nothing to the noise floor
    6. size cap: <=1500 changed lines and <=30 files
    7. per-repo cap: 15

  Why merged PRs count as "known-good": merging into these projects means the
  change survived human review and CI that installs + imports the package.
  SURVIVORSHIP is the cleanliness guarantee. `bench verify` is QA on the
  mining, not the guarantee -- see verify.py for why that distinction matters.

Auth: reads a GitHub token from $GITHUB_TOKEN. Unauthenticated REST is capped
at 60 requests/hour (a full mining run needs a few hundred); a token raises it
to 5,000/hour. Missing token -> loud warning + unauthenticated fallback.

Stdlib only (urllib) -- no third-party HTTP client, no `gh` CLI dependency.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from collections import Counter
from datetime import datetime, timedelta, timezone
from pathlib import Path

from .diffscan import diff_stats, iter_added_lines, post_image_paths

API = "https://api.github.com"
USER_AGENT = "safecommit-bench-miner"

# Approved gate constants
RECENCY_DAYS = 730        # gate 2
MAX_FILES = 30            # gate 6
MAX_CHANGED_LINES = 1500  # gate 6
PER_REPO_CAP = 15         # gate 7

# Gate 5 regexes. These decide INCLUSION only ("can this PR exercise the
# detector at all?"); precise extraction is the verifier's/detector's job.
IMPORT_LINE = re.compile(r"^\s*(?:import|from)\s+[A-Za-z_.]")
DEP_MANIFEST = re.compile(
    r"(?:^|/)(?:requirements[^/]*\.txt|pyproject\.toml|setup\.py|setup\.cfg)$"
)
# package name [extras], then end-of-line, a version specifier, or an env marker
REQ_SPEC = re.compile(
    r"^[A-Za-z0-9][A-Za-z0-9._-]*(?:\[[^\]]+\])?\s*(?:(?:===|==|>=|<=|~=|!=|>|<)\S*|;.*)?$"
)


# --------------------------------------------------------------------------- #
# HTTP helpers
# --------------------------------------------------------------------------- #
def _token() -> str | None:
    tok = os.environ.get("GITHUB_TOKEN", "").strip()
    return tok or None


def _headers(token: str | None, accept: str) -> dict[str, str]:
    h = {
        "Accept": accept,
        "User-Agent": USER_AGENT,
        "X-GitHub-Api-Version": "2022-11-28",
    }
    if token:
        h["Authorization"] = f"Bearer {token}"
    return h


def _request(url: str, token: str | None, accept: str) -> str:
    req = urllib.request.Request(url, headers=_headers(token, accept))
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        remaining = exc.headers.get("X-RateLimit-Remaining") if exc.headers else None
        if exc.code in (403, 429) and remaining == "0":
            reset = exc.headers.get("X-RateLimit-Reset") if exc.headers else None
            hint = ""
            if reset:
                secs = max(0, int(reset) - int(time.time()))
                hint = f" (resets in ~{secs // 60} min)"
            raise RuntimeError(
                "GitHub rate limit hit"
                + hint
                + (
                    ". Set GITHUB_TOKEN to raise the cap from 60 to 5,000 req/hr."
                    if not token
                    else "."
                )
            ) from exc
        raise


def _get_json(url: str, token: str | None):
    return json.loads(_request(url, token, "application/vnd.github+json"))


# --------------------------------------------------------------------------- #
# Gates
# --------------------------------------------------------------------------- #
def _merged_within(pr: dict, cutoff: datetime) -> bool:
    ts = pr.get("merged_at")
    if not ts:
        return False
    return datetime.fromisoformat(ts.replace("Z", "+00:00")) >= cutoff


def _is_bot(pr: dict) -> bool:
    user = pr.get("user") or {}
    login = str(user.get("login") or "")
    return user.get("type") == "Bot" or login.endswith("[bot]")


def diff_touches_python(diff_text: str) -> bool:
    return any(p.endswith(".py") for p in post_image_paths(diff_text))


def _is_requirement_like(path: str, content: str) -> bool:
    s = content.strip()
    if not s or s.startswith("#"):
        return False
    if path.endswith(".txt"):
        # requirements.txt: skip option lines (-r/-e/--hash) and URL refs
        return not s.startswith("-") and "://" not in s
    # pyproject/setup: accept only lines shaped like a PEP 508-ish spec
    s = s.strip(",").strip("\"'")
    return bool(REQ_SPEC.match(s))


def diff_adds_import_or_dep(diff_text: str) -> bool:
    for path, content in iter_added_lines(diff_text):
        if path is None:
            continue
        if path.endswith(".py"):
            if IMPORT_LINE.match(content):
                return True
        elif DEP_MANIFEST.search(path):
            if _is_requirement_like(path, content):
                return True
    return False


# --------------------------------------------------------------------------- #
# Mining
# --------------------------------------------------------------------------- #
def _iter_closed_prs(repo: str, token: str | None, max_pages: int):
    """Yield closed PRs for `repo`, newest-updated first, page by page."""
    for page in range(1, max_pages + 1):
        url = (
            f"{API}/repos/{repo}/pulls"
            f"?state=closed&sort=updated&direction=desc&per_page=100&page={page}"
        )
        batch = _get_json(url, token)
        if not batch:
            return
        yield from batch


def fetch_pr_diff(repo: str, number: int, token: str | None) -> str:
    url = f"{API}/repos/{repo}/pulls/{number}"
    return _request(url, token, "application/vnd.github.v3.diff")


def _existing_ids(manifest: Path) -> set[str]:
    if not manifest.exists():
        return set()
    ids: set[str] = set()
    for line in manifest.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            try:
                ids.add(json.loads(line)["id"])
            except (json.JSONDecodeError, KeyError):
                pass
    return ids


def _write_mining_report(out: Path, repos_stats: dict[str, dict]) -> None:
    """Merge this run's per-gate counts into mining-report.json (rerun-safe)."""
    path = out / "mining-report.json"
    existing: dict = {}
    if path.exists():
        try:
            existing = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            existing = {}
    repos = existing.get("repos", {})
    for repo, stats in repos_stats.items():
        merged = Counter(repos.get(repo, {}))
        merged.update(stats)
        repos[repo] = dict(merged)
    payload = {
        "criteria": {
            "recency_days": RECENCY_DAYS,
            "max_files": MAX_FILES,
            "max_changed_lines": MAX_CHANGED_LINES,
            "per_repo_cap": PER_REPO_CAP,
            "gates": [
                "merged",
                f"merged within {RECENCY_DAYS} days",
                "human-authored (no bots)",
                "touches >=1 .py file",
                "adds import line or dependency requirement",
                f"size <= {MAX_CHANGED_LINES} changed lines / {MAX_FILES} files",
                f"per-repo cap {PER_REPO_CAP}",
            ],
        },
        "repos": repos,
        "updated": datetime.now(timezone.utc).isoformat(timespec="seconds"),
    }
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
def add_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--repo", action="append", required=True, help="owner/name (repeatable)"
    )
    parser.add_argument(
        "--count",
        type=int,
        default=12,
        help=f"qualifying PRs to keep per repo (hard-capped at {PER_REPO_CAP})",
    )
    parser.add_argument("--out", default="corpus/known-good", help="output corpus dir")
    parser.add_argument(
        "--max-pages",
        type=int,
        default=6,
        help="closed-PR pages to examine per repo (100 PRs/page)",
    )
    parser.add_argument(
        "--sleep", type=float, default=0.25, help="seconds between requests"
    )


def run_cli(args: argparse.Namespace) -> int:
    token = _token()
    if token is None:
        print(
            "WARNING: no GITHUB_TOKEN found. Falling back to UNAUTHENTICATED GitHub "
            "REST, which is limited to 60 requests/hour and will choke before "
            "mining a full corpus. Export GITHUB_TOKEN (e.g. via .env) to raise "
            "the cap to 5,000/hr.",
            file=sys.stderr,
        )

    cutoff = datetime.now(timezone.utc) - timedelta(days=RECENCY_DAYS)
    target = min(args.count, PER_REPO_CAP)

    out = Path(args.out)
    diffs_dir = out / "diffs"
    diffs_dir.mkdir(parents=True, exist_ok=True)
    manifest = out / "manifest.jsonl"
    seen = _existing_ids(manifest)

    all_stats: dict[str, dict] = {}
    with manifest.open("a", encoding="utf-8") as mf:
        for repo in args.repo:
            stats: Counter = Counter()
            kept = 0  # cases of this repo counted toward the cap (incl. reruns)
            try:
                for pr in _iter_closed_prs(repo, token, args.max_pages):
                    if kept >= target:
                        break
                    if not pr.get("merged_at"):
                        stats["skipped_not_merged"] += 1
                        continue
                    stats["examined_merged"] += 1
                    if not _merged_within(pr, cutoff):
                        stats["rejected_too_old"] += 1
                        continue
                    if _is_bot(pr):
                        stats["rejected_bot"] += 1
                        continue
                    number = pr["number"]
                    case_id = f"{repo.replace('/', '__')}__pr{number}"
                    if case_id in seen:
                        stats["already_mined"] += 1
                        kept += 1
                        continue
                    if args.sleep:
                        time.sleep(args.sleep)
                    try:
                        diff = fetch_pr_diff(repo, number, token)
                    except urllib.error.HTTPError:
                        # e.g. 406 for diffs too large to render; skip, don't abort
                        stats["rejected_diff_unavailable"] += 1
                        continue
                    if not diff_touches_python(diff):
                        stats["rejected_no_python_file"] += 1
                        continue
                    nfiles, nlines = diff_stats(diff)
                    if nfiles > MAX_FILES or nlines > MAX_CHANGED_LINES:
                        stats["rejected_too_large"] += 1
                        continue
                    if not diff_adds_import_or_dep(diff):
                        stats["rejected_no_import_or_dep"] += 1
                        continue
                    (diffs_dir / f"{case_id}.diff").write_text(diff, encoding="utf-8")
                    user = pr.get("user") or {}
                    row = {
                        "id": case_id,
                        "diff": f"diffs/{case_id}.diff",
                        "label": "clean",
                        "source": {
                            "repo": repo,
                            "pr": number,
                            "sha": pr.get("merge_commit_sha"),
                            "url": pr.get("html_url"),
                            "merged_at": pr.get("merged_at"),
                            "author": user.get("login"),
                        },
                    }
                    mf.write(json.dumps(row) + "\n")
                    mf.flush()
                    seen.add(case_id)
                    kept += 1
                    stats["kept_new"] += 1
                    print(f"  + {case_id}")
            except (RuntimeError, urllib.error.URLError) as exc:
                print(f"ERROR while mining {repo}: {exc}", file=sys.stderr)
                all_stats[repo] = dict(stats)
                _write_mining_report(out, all_stats)
                return 1
            all_stats[repo] = dict(stats)
            print(
                f"{repo}: kept {stats['kept_new']} new, {kept} total toward cap "
                f"(examined {stats['examined_merged']} merged PRs)"
            )

    _write_mining_report(out, all_stats)
    total = len(_existing_ids(manifest))
    print(f"\ncorpus now holds {total} case(s); per-gate counts in {out / 'mining-report.json'}")
    return 0
