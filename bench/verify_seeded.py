"""Re-verify that every seeded hallucination name is STILL absent from PyPI.

The seeded corpus's validity decays: a typosquat-shaped name can be registered
at any moment (defensively, or maliciously -- slopsquatting). "Re-verify before
publishing benchmark numbers" is therefore a mechanism, not a reminder: run
this command; exit code 1 means at least one seeded name now EXISTS and its
case must be re-authored with a fresh verified-absent name before any numbers
are published.

Always re-fetches (bypasses local cache reads): a cached 404 is exactly the
thing this tool exists to distrust.
"""
from __future__ import annotations

import argparse
import json
import sys
from datetime import date
from pathlib import Path

from .corpus import load_corpus
from .verify import PyPIOracle


def add_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--corpus", default="corpus/seeded", help="seeded corpus dir")
    parser.add_argument(
        "--cache-dir", default=".bench-cache/pypi", help="PyPI response cache (write-through)"
    )
    parser.add_argument("--sleep", type=float, default=0.25, help="pause between PyPI calls")
    parser.add_argument(
        "--update-manifest",
        action="store_true",
        help="on a fully-absent PASS, stamp each truth's verified_absent with today's date",
    )


def run_cli(args: argparse.Namespace) -> int:
    corpus_dir = Path(args.corpus)
    cases = load_corpus(corpus_dir)
    oracle = PyPIOracle(Path(args.cache_dir), sleep=args.sleep)
    today = date.today().isoformat()

    checked = 0
    failures: list[tuple[str, str]] = []
    for case in cases:
        for t in case.truths:
            ok, ev = oracle.exists(t.name, refresh=True)
            checked += 1
            mark = "NOW EXISTS  <-- benchmark validity broken" if ok else "still absent"
            print(f"  {case.id:<12} {t.name:<22} {mark}  ({ev})")
            if ok:
                failures.append((case.id, t.name))

    print(f"\nchecked {checked} seeded name(s) FRESH against PyPI on {today}")
    if failures:
        print(f"FAIL: {len(failures)} name(s) now registered — re-author before publishing:")
        for cid, name in failures:
            print(f"  - {cid}: {name}")
        return 1

    print("PASS: every seeded name is still absent from PyPI")
    if args.update_manifest:
        manifest = corpus_dir / "manifest.jsonl"
        lines: list[str] = []
        for raw in manifest.read_text(encoding="utf-8").splitlines():
            s = raw.strip()
            if not s or s.startswith("#"):
                lines.append(raw)
                continue
            row = json.loads(s)
            for t in row.get("truth", []):
                t["verified_absent"] = today
            lines.append(json.dumps(row))
        manifest.write_text("\n".join(lines) + "\n", encoding="utf-8")
        print(f"manifest verified_absent stamps updated to {today}")
    return 0
