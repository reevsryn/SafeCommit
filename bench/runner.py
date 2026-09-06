"""Run a tool-under-test as a subprocess, and orchestrate a full evaluation.

The tool contract (language-agnostic on purpose):

    * INPUT : a unified diff on stdin
    * OUTPUT: a JSON array of findings on stdout, each
              {name, file?, line?, kind?, message?}
    * exit code is informational. We grade stdout, not the exit code, because a
      real CI tool will exit non-zero precisely *when* it has findings.
"""
from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass, field
from pathlib import Path

from .corpus import load_corpus
from .model import Counts, Finding, Metrics, metrics_from_counts
from .score import MATCH_FILE, CaseResult, aggregate, aggregate_kinds, match_case, parse_findings


@dataclass
class ToolRun:
    findings: list[Finding]
    returncode: int
    stderr: str
    error: str | None = None  # set if the tool crashed or violated the contract


def run_tool(tool_argv: list[str], diff_text: str, timeout: float = 60.0) -> ToolRun:
    """Feed one diff to the tool on stdin; parse its stdout JSON into findings."""
    try:
        proc = subprocess.run(
            tool_argv,
            input=diff_text,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        return ToolRun(findings=[], returncode=-1, stderr="", error=str(exc))

    try:
        findings = parse_findings(proc.stdout)
    except (ValueError, json.JSONDecodeError) as exc:
        return ToolRun(
            findings=[],
            returncode=proc.returncode,
            stderr=proc.stderr,
            error=f"invalid tool output: {exc}",
        )

    # Crash heuristic: non-zero exit with no stdout but some stderr looks like a
    # real failure, not a normal "exit non-zero because there were findings".
    if proc.returncode != 0 and not proc.stdout.strip() and proc.stderr.strip():
        last = proc.stderr.strip().splitlines()[-1]
        return ToolRun(
            findings=[],
            returncode=proc.returncode,
            stderr=proc.stderr,
            error=f"tool exited {proc.returncode}: {last}",
        )

    return ToolRun(findings=findings, returncode=proc.returncode, stderr=proc.stderr)


@dataclass
class CorpusResult:
    name: str
    num_cases: int
    num_findings: int
    counts: Counts
    metrics: Metrics
    noise: int  # total findings emitted on clean cases (every one is a false positive)
    # truth kind -> (hits, total). R3: a blended recall number can hide an
    # entirely broken detection path.
    kinds: dict = field(default_factory=dict)
    # truths missed specifically because the finding carried no location (R2)
    unlocated: int = 0
    case_results: list[CaseResult] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)


@dataclass
class EvalReport:
    tool: str
    match: str = MATCH_FILE
    corpora: list[CorpusResult] = field(default_factory=list)

    @property
    def total(self) -> CorpusResult:
        counts = Counts()
        num_cases = num_findings = noise = unlocated = 0
        errors: list[str] = []
        kinds: dict = {}
        for c in self.corpora:
            counts = counts + c.counts
            num_cases += c.num_cases
            num_findings += c.num_findings
            noise += c.noise
            unlocated += c.unlocated
            errors.extend(c.errors)
            for k, (h, t) in c.kinds.items():
                ph, pt = kinds.get(k, (0, 0))
                kinds[k] = (ph + h, pt + t)
        return CorpusResult(
            name="TOTAL",
            num_cases=num_cases,
            num_findings=num_findings,
            counts=counts,
            metrics=metrics_from_counts(counts),
            noise=noise,
            kinds=kinds,
            unlocated=unlocated,
            errors=errors,
        )


def evaluate(
    tool_argv: list[str],
    corpus_dirs: list[str | Path],
    timeout: float = 60.0,
    match: str = MATCH_FILE,
) -> EvalReport:
    """Run `tool_argv` over every case in every corpus and score the results."""
    report = EvalReport(tool=" ".join(tool_argv), match=match)
    for corpus_dir in corpus_dirs:
        cases = load_corpus(corpus_dir)
        case_results: list[CaseResult] = []
        errors: list[str] = []
        num_findings = 0
        noise = 0
        for case in cases:
            # {context} lets one tool command carry per-case repo context. An
            # empty string when a case has none, which the detector reads as
            # "no checkout available" -- the degraded mode.
            argv = [a.replace("{context}", case.context or "") for a in tool_argv]
            run = run_tool(argv, case.read_diff(), timeout=timeout)
            if run.error:
                errors.append(f"{case.id}: {run.error}")
            num_findings += len(run.findings)
            if case.is_clean:
                noise += len(run.findings)
            case_results.append(match_case(case.id, run.findings, case.truths, match=match))
        counts = aggregate(case_results)
        unlocated = sum(len(cr.unlocated_names) for cr in case_results)
        report.corpora.append(
            CorpusResult(
                name=Path(corpus_dir).name,
                num_cases=len(cases),
                num_findings=num_findings,
                counts=counts,
                metrics=metrics_from_counts(counts),
                noise=noise,
                kinds=aggregate_kinds(case_results),
                unlocated=unlocated,
                case_results=case_results,
                errors=errors,
            )
        )
    return report
