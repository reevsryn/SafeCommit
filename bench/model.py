"""Shared data types for the eval harness.

These are deliberately tiny and dependency-free. Keeping the shared vocabulary
(Finding / Truth) and the metric math in one place means score.py and corpus.py
can share it without importing each other.
"""
from __future__ import annotations

import re
from dataclasses import dataclass

# PEP 503 name normalization. PyPI treats "Foo_Bar", "foo-bar" and "foo.bar" as
# the SAME project. We normalize on both sides before matching so a finding that
# says "pandas_helpers" matches a seeded truth written "pandas-helpers".
_NORMALIZE_RE = re.compile(r"[-_.]+")


def normalize_name(name: str) -> str:
    """Normalize a package/import name per PEP 503: lowercase, runs of -_. -> -."""
    return _NORMALIZE_RE.sub("-", name.strip()).lower()


@dataclass(frozen=True)
class Finding:
    """One thing a tool-under-test claims is wrong. `name` is the match key."""

    name: str
    file: str | None = None
    line: int | None = None
    kind: str = ""
    message: str = ""


@dataclass(frozen=True)
class Truth:
    """One known hallucination in a seeded case (the ground-truth answer)."""

    name: str
    file: str | None = None
    kind: str = ""


@dataclass(frozen=True)
class Counts:
    """Raw confusion-matrix tallies for emitted findings vs. ground truth."""

    tp: int = 0
    fp: int = 0
    fn: int = 0

    def __add__(self, other: "Counts") -> "Counts":
        return Counts(self.tp + other.tp, self.fp + other.fp, self.fn + other.fn)


@dataclass(frozen=True)
class Metrics:
    precision: float | None
    recall: float | None
    f1: float | None


def metrics_from_counts(c: Counts) -> Metrics:
    """Turn raw TP/FP/FN into precision / recall / F1.

    Conventions (written down so the numbers are unambiguous):

      * precision = TP / (TP + FP). Undefined (None) when the tool emitted no
        findings at all (TP + FP == 0) -> "it never spoke, so it was never wrong".
      * recall    = TP / (TP + FN). Undefined (None) when there are no truths to
        find (TP + FN == 0) -> e.g. a pure known-good corpus has nothing to recall.
      * F1        = harmonic mean of P and R. Undefined (None) when either P or R
        is undefined (no findings, or nothing to find); 0.0 when both are defined
        but zero (it fired and was entirely wrong); otherwise 2PR / (P + R).
    """
    p = c.tp / (c.tp + c.fp) if (c.tp + c.fp) > 0 else None
    r = c.tp / (c.tp + c.fn) if (c.tp + c.fn) > 0 else None
    if p is None or r is None:
        f1: float | None = None
    elif (p + r) == 0:
        f1 = 0.0
    else:
        f1 = 2 * p * r / (p + r)
    return Metrics(precision=p, recall=r, f1=f1)
