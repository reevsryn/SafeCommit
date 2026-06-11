"""`python3 -m bench <subcommand>` entry point.

Subcommands:
    eval           run a tool over one or more corpora and print precision/recall/noise
    mine           (step 2) fetch real merged Python PRs into a known-good corpus
    verify         (step 2) QA a mined corpus; quarantine unresolved cases for review
    verify-seeded  (step 3) re-check every seeded name is still absent from PyPI
"""
from __future__ import annotations

import argparse
import shlex
import sys

from . import mine as mine_mod
from . import verify as verify_mod
from . import verify_seeded as verify_seeded_mod
from .report import render_json, render_text
from .runner import evaluate


def cmd_eval(args: argparse.Namespace) -> int:
    tool_argv = shlex.split(args.tool)
    report = evaluate(tool_argv, args.corpus, timeout=args.timeout)
    print(render_json(report) if args.json else render_text(report))
    if args.fail_on_noise and report.total.noise > 0:
        return 1
    return 0


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog="bench", description="SafeCommit eval harness")
    sub = p.add_subparsers(dest="command", required=True)

    e = sub.add_parser("eval", help="run a tool over corpora and score it")
    e.add_argument(
        "--tool",
        required=True,
        help="tool command, e.g. 'python3 tools/dummy/never_fire.py'",
    )
    e.add_argument(
        "--corpus", action="append", required=True, help="corpus dir (repeatable)"
    )
    e.add_argument("--json", action="store_true", help="emit JSON instead of a table")
    e.add_argument("--timeout", type=float, default=60.0, help="per-case timeout (s)")
    e.add_argument(
        "--fail-on-noise",
        action="store_true",
        help="exit 1 if the tool fires on any clean case",
    )
    e.set_defaults(func=cmd_eval)

    m = sub.add_parser(
        "mine", help="(step 2) mine merged Python PRs into a known-good corpus"
    )
    mine_mod.add_arguments(m)
    m.set_defaults(func=mine_mod.run_cli)

    v = sub.add_parser(
        "verify",
        help="(step 2) QA a mined corpus against stdlib/first-party/PyPI; "
        "quarantine unresolved cases",
    )
    verify_mod.add_arguments(v)
    v.set_defaults(func=verify_mod.run_cli)

    vs = sub.add_parser(
        "verify-seeded",
        help="(step 3) fresh-check that every seeded hallucination name is "
        "still absent from PyPI; exit 1 if any got registered",
    )
    verify_seeded_mod.add_arguments(vs)
    vs.set_defaults(func=verify_seeded_mod.run_cli)

    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
