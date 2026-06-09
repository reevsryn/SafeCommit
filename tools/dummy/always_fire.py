#!/usr/bin/env python3
"""Dummy tool: fires on EVERY imported module on added (+) lines.

NOT a detector -- it has no idea what exists. Its job is to show the harness
reports maximum recall together with lots of false positives (terrible
precision, high noise) -- the caricature of a noisy incumbent reviewer.
"""
import json
import re
import sys

ADDED = re.compile(r"^\+(?!\+\+)")  # an added line, but not the "+++" file header
IMPORT = re.compile(r"^\s*import\s+([a-zA-Z0-9_]+)")
FROM = re.compile(r"^\s*from\s+([a-zA-Z0-9_]+)")


def main() -> None:
    names: list[str] = []
    seen: set[str] = set()
    for raw in sys.stdin.read().splitlines():
        if not ADDED.match(raw):
            continue
        content = raw[1:]  # strip the leading '+'
        m = IMPORT.match(content) or FROM.match(content)
        if m:
            name = m.group(1)
            if name not in seen:
                seen.add(name)
                names.append(name)
    findings = [
        {
            "name": n,
            "kind": "import",
            "message": f"package '{n}' flagged (always-fire dummy)",
        }
        for n in names
    ]
    json.dump(findings, sys.stdout)


main()
