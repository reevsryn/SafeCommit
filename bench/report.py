"""Render an EvalReport as a human-readable table or as JSON."""
from __future__ import annotations

import json

from .runner import CorpusResult, EvalReport

_HEADER = (
    f"{'corpus':<16}{'cases':>6}{'findings':>10}"
    f"{'TP':>5}{'FP':>5}{'FN':>5}"
    f"{'precision':>11}{'recall':>9}{'F1':>8}"
)


def _pct(x: float | None) -> str:
    return "—" if x is None else f"{x * 100:.1f}%"


def _row(c: CorpusResult) -> str:
    return (
        f"{c.name:<16}{c.num_cases:>6}{c.num_findings:>10}"
        f"{c.counts.tp:>5}{c.counts.fp:>5}{c.counts.fn:>5}"
        f"{_pct(c.metrics.precision):>11}{_pct(c.metrics.recall):>9}{_pct(c.metrics.f1):>8}"
    )


def render_text(report: EvalReport) -> str:
    lines = [f"SafeCommit bench — tool: {report.tool}", "", _HEADER, "-" * len(_HEADER)]
    for c in report.corpora:
        lines.append(_row(c))
    lines.append("-" * len(_HEADER))
    lines.append(_row(report.total))
    lines.append("")
    lines.append(
        f"NOISE (findings on known-good / clean cases): {report.total.noise}   ← target: 0"
    )

    errs = report.total.errors
    if errs:
        lines.append("")
        lines.append("tool errors:")
        for e in errs:
            lines.append(f"  ! {e}")
    return "\n".join(lines)


def _corpus_dict(c: CorpusResult) -> dict:
    return {
        "corpus": c.name,
        "cases": c.num_cases,
        "findings": c.num_findings,
        "tp": c.counts.tp,
        "fp": c.counts.fp,
        "fn": c.counts.fn,
        "precision": c.metrics.precision,
        "recall": c.metrics.recall,
        "f1": c.metrics.f1,
        "noise": c.noise,
        "errors": c.errors,
    }


def render_json(report: EvalReport) -> str:
    payload = {
        "tool": report.tool,
        "corpora": [_corpus_dict(c) for c in report.corpora],
        "total": _corpus_dict(report.total),
    }
    return json.dumps(payload, indent=2)
