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
    findings: list[dict] = []
    seen: set[str] = set()
    path: str | None = None
    for raw in sys.stdin.read().splitlines():
        # Track the post-image path so findings carry a location. Scoring is
        # file-aware (see bench/score.py, R2): a finding with no file cannot
        # credit a truth that declares one.
        if raw.startswith("+++ b/"):
            path = raw[6:]
            continue
        if raw.startswith("+++ "):
            path = None
            continue
        if not ADDED.match(raw):
            continue
        content = raw[1:]  # strip the leading '+'
        m = IMPORT.match(content) or FROM.match(content)
        if m:
            name = m.group(1)
            if name not in seen:
                seen.add(name)
                findings.append(
                    {
                        "name": name,
                        "file": path,
                        "kind": "import",
                        "message": f"package '{name}' flagged (always-fire dummy)",
                    }
                )
    json.dump(findings, sys.stdout)


main()
