#!/usr/bin/env python3
"""Dummy tool: NEVER fires. Establishes the noise floor (0 findings, ever).

Not a detector -- a reference point. On the known-good corpus it should produce
0 noise; on the seeded corpus it should produce 0 recall. Reads (and discards)
the diff on stdin and prints an empty JSON array.
"""
import json
import sys

sys.stdin.read()
json.dump([], sys.stdout)
