"""Capture the repository layout each corpus case would see from a checkout.

# Why this exists

The detector resolves first-party names against the repository it is scanning.
In production that is a checkout: the CLI runs inside the repo and the GitHub
Action runs after actions/checkout. The benchmark corpora are diffs with no
checkout, so without this the benchmark can only ever measure the DEGRADED
mode -- and any improvement to first-party resolution would be invisible to it.

That is not hypothetical. The first holdout scored 7 false positives, every one
a first-party name (PHASE1-NOTES.md R6). Fixing that is pointless if the
benchmark cannot see the fix.

So we capture, once, the same listing a checkout would provide, and commit it
with the corpus. `internal/repoindex` reads it through a second backend that
runs the identical matching logic -- a benchmark-only code path would measure
something production does not do.

# What is captured, and the rules it mirrors

Exactly what internal/repoindex.FromFilesystem derives:

  top_level  the two standard sys.path roots --
             (1) entries at the repository root
             (2) children of any src/ directory, at any depth
             plus (3) PACKAGE ROOTS: a directory with __init__.py whose parent
             has none. The parent check matters: without it every subpackage in
             a monorepo (utils, models, config) becomes a top-level name and a
             hallucination colliding with one would be silently suppressed.

  siblings   directory -> module names in it, for the directories the corpus's
             diffs actually touch. Python puts a script's own directory on
             sys.path, so `from extract_permissions import x` resolves for any
             file beside it. Restricted to touched directories to keep the
             capture small.

# Approximation, stated plainly

The tree is read at the repository's current HEAD, not at each PR's merge
commit: one request per repo instead of one per case, and the layout of a
project changes far more slowly than its contents. For first-party resolution
this is adequate, but it IS an approximation, and a name that appeared or moved
since a PR merged could be classified differently than it would have been then.
"""
from __future__ import annotations

import argparse
import json
import sys
import urllib.error
from collections import defaultdict
from pathlib import Path

from .corpus import load_corpus
from .diffscan import post_image_paths
from .mine import API, _get_json, _token


def _slug(repo: str) -> str:
    return repo.replace("/", "__")


def build_index(paths: list[str], types: dict[str, str], touched_dirs: set[str]) -> dict:
    """Derive top_level and siblings from a flat repository tree listing."""
    blobs = {p for p, t in types.items() if t == "blob"}
    dirs = {p for p, t in types.items() if t == "tree"}

    def module_name(name: str, is_dir: bool) -> str | None:
        if not name or name.startswith("."):
            return None
        if is_dir:
            return name
        if name.endswith(".py"):
            return name[:-3]
        if name.endswith(".pyi"):
            return name[:-4]
        return None

    def has_init(d: str) -> bool:
        return (f"{d}/__init__.py" if d else "__init__.py") in blobs

    top: set[str] = set()
    for p in paths:
        parent, _, base = p.rpartition("/")
        is_dir = p in dirs
        name = module_name(base, is_dir)
        if not name:
            continue
        # (1) repo root, (2) child of any src/ directory
        if parent == "" or parent.rsplit("/", 1)[-1] == "src":
            top.add(name)
        # (3) package root: has __init__.py, parent does not
        if is_dir and has_init(p) and not has_init(parent):
            top.add(name)

    siblings: dict[str, set[str]] = defaultdict(set)
    for p in paths:
        parent, _, base = p.rpartition("/")
        if parent not in touched_dirs:
            continue
        name = module_name(base, p in dirs)
        if name:
            siblings[parent].add(name)

    return {
        "top_level": sorted(top),
        "siblings": {d: sorted(v) for d, v in sorted(siblings.items())},
    }


def add_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--corpus", required=True, help="corpus dir to capture context for")
    parser.add_argument(
        "--ref", default="HEAD", help="git ref to read each repo's tree at (default HEAD)"
    )


def run_cli(args: argparse.Namespace) -> int:
    corpus_dir = Path(args.corpus)
    cases = load_corpus(corpus_dir)
    token = _token()
    if token is None:
        print("warning: no GITHUB_TOKEN; tree requests are rate-limited", file=sys.stderr)

    by_repo: dict[str, list] = defaultdict(list)
    for c in cases:
        repo = c.source.get("repo")
        if repo:
            by_repo[repo].append(c)

    out_dir = corpus_dir / "context"
    out_dir.mkdir(parents=True, exist_ok=True)
    written: dict[str, str] = {}

    print(f"capturing repo context for {len(cases)} case(s) across {len(by_repo)} repo(s)")
    for repo, repo_cases in sorted(by_repo.items()):
        touched: set[str] = set()
        for c in repo_cases:
            for p in post_image_paths(c.read_diff()):
                parent = p.rpartition("/")[0]
                touched.add(parent)
        try:
            tree = _get_json(f"{API}/repos/{repo}/git/trees/{args.ref}?recursive=1", token)
        except (urllib.error.URLError, urllib.error.HTTPError, RuntimeError) as exc:
            print(f"  !! {repo}: tree fetch failed ({exc}); skipping", file=sys.stderr)
            continue

        entries = tree.get("tree", [])
        types = {e["path"]: e.get("type", "") for e in entries}
        idx = build_index(list(types), types, touched)
        idx["_repo"] = repo
        idx["_ref"] = args.ref
        idx["_truncated"] = bool(tree.get("truncated"))
        if idx["_truncated"]:
            print(f"  !! {repo}: GitHub truncated the tree; context is partial", file=sys.stderr)

        rel = f"context/{_slug(repo)}.json"
        (corpus_dir / rel).write_text(json.dumps(idx, indent=1) + "\n", encoding="utf-8")
        written[repo] = rel
        print(
            f"  {repo:<32} {len(entries):>7} tree entries -> "
            f"{len(idx['top_level'])} top-level, {len(idx['siblings'])} dirs"
            + ("  [TRUNCATED]" if idx["_truncated"] else "")
        )

    # Point each manifest row at its repo's context file.
    manifest = corpus_dir / "manifest.jsonl"
    out_lines: list[str] = []
    tagged = 0
    for raw in manifest.read_text(encoding="utf-8").splitlines():
        s = raw.strip()
        if not s or s.startswith("#"):
            out_lines.append(raw)
            continue
        row = json.loads(s)
        rel = written.get(row.get("source", {}).get("repo", ""))
        if rel:
            row["context"] = rel
            tagged += 1
        out_lines.append(json.dumps(row))
    manifest.write_text("\n".join(out_lines) + "\n", encoding="utf-8")
    print(f"\ntagged {tagged}/{len(cases)} case(s) with a context file")
    return 0
