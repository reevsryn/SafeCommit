#!/usr/bin/env python3
"""Dummy tool: fires only on a fixed list of known-fake names.

NOT a real detector -- it "knows the answers" for a corpus. Its only job is to
show that the harness reports a clean, ideal result (0 noise, full recall,
100% precision) when a tool flags exactly the seeded hallucinations and
nothing else. The real SafeCommit will reach this behavior by VERIFYING names
against PyPI, not by hardcoding them.

Matches two added-line shapes: import statements (`import x`, `from x import`)
and requirement-style lines (`name==1.0`, `"name>=2",`, bare `name`) so seeded
truths planted in requirements.txt/pyproject.toml are reachable too.

(Filename note: this file must NOT be called keyword.py -- that shadows the
stdlib `keyword` module, which the interpreter imports during startup,
breaking every script run from this directory.)

Override the fake list via SAFECOMMIT_FAKES="a,b,c".
"""
import json
import os
import re
import sys

DEFAULT_FAKES = {"reqursts", "beautifulsuop", "pandas_helpers"}

ADDED = re.compile(r"^\+(?!\+\+)")
IMPORT = re.compile(r"^\s*import\s+([a-zA-Z0-9_]+)")
FROM = re.compile(r"^\s*from\s+([a-zA-Z0-9_]+)")
# name then a version specifier or end-of-line (a bare '=' kwarg does NOT match)
REQLINE = re.compile(
    r"^\s*([A-Za-z0-9][A-Za-z0-9._-]+)\s*(?:(?:===|==|>=|<=|~=|!=|>|<)\S*\s*,?\s*$|$)"
)
_NORM = re.compile(r"[-_.]+")


def norm(s: str) -> str:
    return _NORM.sub("-", s.strip()).lower()


def main() -> None:
    env = os.environ.get("SAFECOMMIT_FAKES", "")
    raw_fakes = [x for x in env.split(",") if x.strip()] or list(DEFAULT_FAKES)
    fakes = {norm(x) for x in raw_fakes}

    findings = []
    seen: set[str] = set()

    path: str | None = None

    def emit(name: str) -> None:
        if norm(name) in fakes and norm(name) not in seen:
            seen.add(norm(name))
            findings.append(
                {
                    "name": name,
                    "file": path,
                    "kind": "import",
                    "message": f"package '{name}' not found on PyPI (did you mean ...?)",
                }
            )

    for raw in sys.stdin.read().splitlines():
        # Track the post-image path: scoring is file-aware (bench/score.py, R2).
        if raw.startswith("+++ b/"):
            path = raw[6:]
            continue
        if raw.startswith("+++ "):
            path = None
            continue
        if not ADDED.match(raw):
            continue
        content = raw[1:]
        m = IMPORT.match(content) or FROM.match(content)
        if m:
            emit(m.group(1))
            continue
        m = REQLINE.match(content.strip().strip(",").strip("\"'"))
        if m:
            emit(m.group(1))
    json.dump(findings, sys.stdout)


main()
