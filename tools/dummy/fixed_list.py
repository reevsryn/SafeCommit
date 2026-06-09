#!/usr/bin/env python3
"""Dummy tool: fires only on a fixed list of known-fake names.

NOT a real detector -- it "knows the answers" for the fixture corpus. Its only
job is to show that the harness reports a clean, ideal result (0 noise, full
recall, 100% precision) when a tool flags exactly the seeded hallucinations and
nothing else. The real SafeCommit will reach this behavior by VERIFYING names
against PyPI, not by hardcoding them.

(Filename note: this file must NOT be called keyword.py -- that shadows the
stdlib `keyword` module, which the interpreter imports during startup, breaking
every script run from this directory.)

Override the fake list for ad-hoc experiments via SAFECOMMIT_FAKES="a,b,c".
"""
import json
import os
import re
import sys

DEFAULT_FAKES = {"reqursts", "beautifulsuop", "pandas_helpers"}

ADDED = re.compile(r"^\+(?!\+\+)")
IMPORT = re.compile(r"^\s*import\s+([a-zA-Z0-9_]+)")
FROM = re.compile(r"^\s*from\s+([a-zA-Z0-9_]+)")
_NORM = re.compile(r"[-_.]+")


def norm(s: str) -> str:
    return _NORM.sub("-", s.strip()).lower()


def main() -> None:
    env = os.environ.get("SAFECOMMIT_FAKES", "")
    raw_fakes = [x for x in env.split(",") if x.strip()] or list(DEFAULT_FAKES)
    fakes = {norm(x) for x in raw_fakes}

    findings = []
    seen: set[str] = set()
    for raw in sys.stdin.read().splitlines():
        if not ADDED.match(raw):
            continue
        content = raw[1:]
        m = IMPORT.match(content) or FROM.match(content)
        if not m:
            continue
        name = m.group(1)
        if norm(name) in fakes and norm(name) not in seen:
            seen.add(norm(name))
            findings.append(
                {
                    "name": name,
                    "kind": "import",
                    "message": f"package '{name}' not found on PyPI (did you mean ...?)",
                }
            )
    json.dump(findings, sys.stdout)


main()
