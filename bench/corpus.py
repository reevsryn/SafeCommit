"""Load a corpus: a directory of diffs + a manifest of labels (ground truth).

A corpus directory looks like:

    <corpus>/
        manifest.jsonl       one JSON object per line (one per case)
        diffs/<id>.diff      the unified diff for each case

Manifest row (known-good / "noise" case):

    {"id": "kg-001", "diff": "diffs/kg-001.diff", "label": "clean",
     "source": {"repo": "psf/requests", "pr": 1234, "url": "..."}}

Manifest row (seeded / "recall" case):

    {"id": "seed-001", "diff": "diffs/seed-001.diff", "label": "hallucination",
     "truth": [{"name": "reqursts", "file": "client.py", "kind": "import"}]}

Blank lines and lines beginning with `#` are ignored (handy for headers/notes).
"""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path

from .model import Truth

LABEL_CLEAN = "clean"
LABEL_HALLUCINATION = "hallucination"


@dataclass
class Case:
    id: str
    diff_path: Path
    label: str
    truths: list[Truth] = field(default_factory=list)
    source: dict = field(default_factory=dict)

    @property
    def is_clean(self) -> bool:
        return self.label == LABEL_CLEAN

    def read_diff(self) -> str:
        return self.diff_path.read_text(encoding="utf-8")


def load_corpus(corpus_dir: str | Path) -> list[Case]:
    root = Path(corpus_dir)
    manifest = root / "manifest.jsonl"
    if not manifest.exists():
        raise FileNotFoundError(f"no manifest.jsonl in {root}")

    cases: list[Case] = []
    for lineno, raw in enumerate(manifest.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"{manifest}:{lineno}: invalid JSON ({exc})") from exc

        label = row.get("label")
        if label not in (LABEL_CLEAN, LABEL_HALLUCINATION):
            raise ValueError(
                f"{manifest}:{lineno}: label must be '{LABEL_CLEAN}' or '{LABEL_HALLUCINATION}'"
            )

        truths = [
            Truth(name=t["name"], file=t.get("file"), kind=t.get("kind", ""))
            for t in row.get("truth", [])
        ]
        if label == LABEL_CLEAN and truths:
            raise ValueError(f"{manifest}:{lineno}: clean cases must not declare truths")

        diff_rel = row.get("diff", f"diffs/{row['id']}.diff")
        cases.append(
            Case(
                id=row["id"],
                diff_path=root / diff_rel,
                label=label,
                truths=truths,
                source=row.get("source", {}),
            )
        )
    return cases
