"""Match findings against ground truth, and aggregate the counts.

# Matching granularity (tightened for publication -- PHASE1-NOTES.md R2)

Phase 0 matched a finding to a truth by PACKAGE NAME alone. That was a
documented simplification, and it had a concrete failure: in `seed-019` the
hallucination is a bad pin in requirements.txt, while the *same name* also
appears in a perfectly legitimate `from dateutil import parser`. A tool that
flagged the legitimate import line was credited with a true positive for a
truth it had not found.

Matching is therefore now FILE-AWARE by default:

    a finding matches a truth when the normalized names are equal AND
    (the truth declares no file, OR the finding's file equals the truth's)

A finding with the right name but the wrong -- or missing -- file no longer
earns credit: the truth counts as a false negative and the finding as a false
positive. That is deliberately strict. A reviewer cannot act on "something
somewhere is wrong", and any tool worth benchmarking against reports a location.

`--match name` restores the old, looser behaviour so the two can be compared;
it must never be used for a published figure.

# Per-kind breakdown (PHASE1-NOTES.md R3)

Recall is also tracked per truth `kind` (import vs requirement), because those
exercise different detector code paths and a blended number can hide one of
them being entirely broken -- which is exactly what a blended 90.5% concealed
at step 3, when manifest recall was actually 0/3.
"""
from __future__ import annotations

import json
from collections import defaultdict
from dataclasses import dataclass, field

from .model import Counts, Finding, Truth, normalize_name

MATCH_NAME = "name"
MATCH_FILE = "file"


def parse_findings(stdout: str) -> list[Finding]:
    """Parse a tool's stdout (a JSON array of findings) into Finding objects.

    Empty/blank stdout means "no findings" ([]). Anything that is not a JSON
    array of objects-with-a-name is a tool-contract violation -> ValueError.
    """
    text = stdout.strip()
    if not text:
        return []
    data = json.loads(text)
    if not isinstance(data, list):
        raise ValueError("tool output must be a JSON array of findings")
    findings: list[Finding] = []
    for i, item in enumerate(data):
        if not isinstance(item, dict) or "name" not in item:
            raise ValueError(f"finding #{i} must be an object with a 'name' field")
        findings.append(
            Finding(
                name=str(item["name"]),
                file=item.get("file"),
                line=item.get("line"),
                kind=item.get("kind", ""),
                message=item.get("message", ""),
            )
        )
    return findings


@dataclass
class CaseResult:
    case_id: str
    counts: Counts
    tp_names: list[str] = field(default_factory=list)
    fp_names: list[str] = field(default_factory=list)
    fn_names: list[str] = field(default_factory=list)
    # recall bookkeeping per truth kind: kind -> [hits, total]
    kind_hits: dict[str, int] = field(default_factory=dict)
    kind_totals: dict[str, int] = field(default_factory=dict)
    # findings whose name matches a truth but that declare no file at all.
    # Reported separately so "missed it" is distinguishable from "found it but
    # could not say where".
    unlocated_names: list[str] = field(default_factory=list)


def _locates(finding: Finding, truth: Truth, mode: str) -> bool:
    """Does `finding` agree with `truth` about WHERE the problem is?"""
    if mode == MATCH_NAME:
        return True
    if not truth.file:
        return True  # nothing to disagree with
    return bool(finding.file) and finding.file == truth.file


def match_case(
    case_id: str,
    findings: list[Finding],
    truths: list[Truth],
    match: str = MATCH_FILE,
) -> CaseResult:
    """Match one case's findings against its truths.

    TP = a truth for which some finding agrees on both name and location.
    FN = a truth no finding located.
    FP = a distinct finding name that did not end up crediting any truth --
         which now includes right-name/wrong-place findings.
    """
    by_name: dict[str, list[Finding]] = defaultdict(list)
    for f in findings:
        by_name[normalize_name(f.name)].append(f)

    tp_names: list[str] = []
    fn_names: list[str] = []
    unlocated: list[str] = []
    kind_hits: dict[str, int] = defaultdict(int)
    kind_totals: dict[str, int] = defaultdict(int)

    for t in truths:
        key = normalize_name(t.name)
        kind = t.kind or "?"
        kind_totals[kind] += 1
        candidates = by_name.get(key, [])
        if any(_locates(f, t, match) for f in candidates):
            tp_names.append(t.name)
            kind_hits[kind] += 1
        else:
            fn_names.append(t.name)
            if candidates and not any(f.file for f in candidates):
                unlocated.append(t.name)

    credited = {normalize_name(n) for n in tp_names}
    fp_names = sorted({f.name for f in findings if normalize_name(f.name) not in credited})

    return CaseResult(
        case_id=case_id,
        counts=Counts(tp=len(tp_names), fp=len(fp_names), fn=len(fn_names)),
        tp_names=sorted(tp_names),
        fp_names=fp_names,
        fn_names=sorted(fn_names),
        kind_hits=dict(kind_hits),
        kind_totals=dict(kind_totals),
        unlocated_names=sorted(unlocated),
    )


def aggregate(results: list[CaseResult]) -> Counts:
    total = Counts()
    for r in results:
        total = total + r.counts
    return total


def aggregate_kinds(results: list[CaseResult]) -> dict[str, tuple[int, int]]:
    """kind -> (hits, total) summed across cases."""
    hits: dict[str, int] = defaultdict(int)
    totals: dict[str, int] = defaultdict(int)
    for r in results:
        for k, v in r.kind_hits.items():
            hits[k] += v
        for k, v in r.kind_totals.items():
            totals[k] += v
    return {k: (hits.get(k, 0), totals[k]) for k in sorted(totals)}
