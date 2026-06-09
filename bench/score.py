"""Match findings against ground truth, and aggregate the counts.

Matching granularity: ONE PACKAGE NAME = ONE UNIT, deduplicated per case and
normalized per PEP 503. Rationale: a seeded truth is "package X is fake"; a tool
says "package X doesn't exist". The natural key is the name. File/line are
reported but not required to match in Phase 0 (the dummy tools don't do real
line mapping). We can tighten to file+line later without touching this contract.
"""
from __future__ import annotations

import json
from dataclasses import dataclass, field

from .model import Counts, Finding, Truth, normalize_name


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


def match_case(case_id: str, findings: list[Finding], truths: list[Truth]) -> CaseResult:
    """Match one case's findings against its truths using normalized names.

    TP = names the tool flagged that are genuinely fake.
    FP = names the tool flagged that are NOT in the truth set. On a clean case
         the truth set is empty, so *every* flagged name is a false positive.
    FN = fake names the tool missed.
    """
    finding_norm = {normalize_name(f.name): f.name for f in findings}
    truth_norm = {normalize_name(t.name): t.name for t in truths}

    tp_keys = finding_norm.keys() & truth_norm.keys()
    fp_keys = finding_norm.keys() - truth_norm.keys()
    fn_keys = truth_norm.keys() - finding_norm.keys()

    return CaseResult(
        case_id=case_id,
        counts=Counts(tp=len(tp_keys), fp=len(fp_keys), fn=len(fn_keys)),
        tp_names=sorted(finding_norm[k] for k in tp_keys),
        fp_names=sorted(finding_norm[k] for k in fp_keys),
        fn_names=sorted(truth_norm[k] for k in fn_keys),
    )


def aggregate(results: list[CaseResult]) -> Counts:
    total = Counts()
    for r in results:
        total = total + r.counts
    return total
