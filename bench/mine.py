"""Mine real, already-merged Python PRs into a known-good ("noise") corpus.

This is STEP 2 of Phase 0. It is built now (with auth wired in from the start)
but is NOT exercised until we actually populate the real corpus.

Auth: reads a GitHub token from $GITHUB_TOKEN. Unauthenticated GitHub REST is
capped at 60 requests/hour, which will choke mining 50-100 PRs; a token raises
the cap to 5,000/hour. If no token is present we warn loudly and fall back to
unauthenticated so a quick smoke test still works.

Stdlib only (urllib) -- no third-party HTTP client, no `gh` CLI dependency.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

API = "https://api.github.com"
USER_AGENT = "safecommit-bench-miner"


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
# Mining
# --------------------------------------------------------------------------- #
def list_merged_prs(repo: str, count: int, token: str | None) -> list[dict]:
    """Return up to `count` merged PRs for `repo` (owner/name), newest first."""
    merged: list[dict] = []
    page = 1
    while len(merged) < count:
        url = (
            f"{API}/repos/{repo}/pulls"
            f"?state=closed&sort=updated&direction=desc&per_page=100&page={page}"
        )
        batch = _get_json(url, token)
        if not batch:
            break
        for pr in batch:
            if pr.get("merged_at"):
                merged.append(pr)
                if len(merged) >= count:
                    break
        page += 1
    return merged


def fetch_pr_diff(repo: str, number: int, token: str | None) -> str:
    url = f"{API}/repos/{repo}/pulls/{number}"
    return _request(url, token, "application/vnd.github.v3.diff")


def diff_touches_python(diff_text: str) -> bool:
    for line in diff_text.splitlines():
        if line.startswith("+++ b/") and line.endswith(".py"):
            return True
    return False


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


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
def add_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--repo", action="append", required=True, help="owner/name (repeatable)"
    )
    parser.add_argument(
        "--count", type=int, default=10, help="max merged PRs to take per repo"
    )
    parser.add_argument("--out", default="corpus/known-good", help="output corpus dir")
    parser.add_argument(
        "--allow-non-python",
        action="store_true",
        help="keep PRs even if they touch no .py files",
    )
    parser.add_argument(
        "--sleep", type=float, default=0.0, help="seconds to sleep between requests"
    )


def run_cli(args: argparse.Namespace) -> int:
    token = _token()
    if token is None:
        print(
            "WARNING: no GITHUB_TOKEN found. Falling back to UNAUTHENTICATED GitHub "
            "REST, which is limited to 60 requests/hour and will likely choke before "
            "mining 50-100 PRs. Export GITHUB_TOKEN to raise the cap to 5,000/hr.",
            file=sys.stderr,
        )

    out = Path(args.out)
    diffs_dir = out / "diffs"
    diffs_dir.mkdir(parents=True, exist_ok=True)
    manifest = out / "manifest.jsonl"
    seen = _existing_ids(manifest)

    written = 0
    with manifest.open("a", encoding="utf-8") as mf:
        for repo in args.repo:
            try:
                prs = list_merged_prs(repo, args.count, token)
            except (RuntimeError, urllib.error.URLError) as exc:
                print(f"ERROR listing {repo}: {exc}", file=sys.stderr)
                return 1
            for pr in prs:
                number = pr["number"]
                case_id = f"{repo.replace('/', '__')}__pr{number}"
                if case_id in seen:
                    continue
                if args.sleep:
                    time.sleep(args.sleep)
                try:
                    diff = fetch_pr_diff(repo, number, token)
                except (RuntimeError, urllib.error.URLError) as exc:
                    print(f"ERROR fetching {repo}#{number}: {exc}", file=sys.stderr)
                    return 1
                if not args.allow_non_python and not diff_touches_python(diff):
                    continue
                (diffs_dir / f"{case_id}.diff").write_text(diff, encoding="utf-8")
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
                    },
                }
                mf.write(json.dumps(row) + "\n")
                seen.add(case_id)
                written += 1
                print(f"  + {case_id}")

    print(f"\nwrote {written} new case(s) to {manifest}")
    return 0
