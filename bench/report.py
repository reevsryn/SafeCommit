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
    lines = [
        f"SafeCommit bench — tool: {report.tool}",
        f"matching: {report.match}"
        + ("  (name-only — NOT valid for a published figure)" if report.match == "name" else "  (finding must agree with the truth's file)"),
        "",
        _HEADER,
        "-" * len(_HEADER),
    ]
    for c in report.corpora:
        lines.append(_row(c))
    lines.append("-" * len(_HEADER))
    lines.append(_row(report.total))
    lines.append("")
    lines.append(
        f"NOISE (findings on known-good / clean cases): {report.total.noise}   ← target: 0"
    )

    kinds = report.total.kinds
    if kinds:
        lines.append("")
        lines.append("recall by detection path (R3 — a blended number can hide a broken path):")
        for kind, (hits, total) in kinds.items():
            pct = "—" if total == 0 else f"{100 * hits / total:.1f}%"
            lines.append(f"  {kind:<14}{hits:>4}/{total:<4} = {pct:>7}")

    if report.total.unlocated:
        lines.append("")
        lines.append(
            f"note: {report.total.unlocated} truth(s) were missed because the tool "
            f"named them but reported no file (R2)"
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
        "kinds": {k: {"hits": h, "total": t} for k, (h, t) in c.kinds.items()},
        "unlocated": c.unlocated,
        "errors": c.errors,
    }


def render_json(report: EvalReport) -> str:
    payload = {
        "tool": report.tool,
        "match": report.match,
        "corpora": [_corpus_dict(c) for c in report.corpora],
        "total": _corpus_dict(report.total),
    }
    return json.dumps(payload, indent=2)
