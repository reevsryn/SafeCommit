"""Low-level unified-diff scanning shared by the miner and the verifier.

This is NOT a diff parser (Phase 1 builds a real one, in Go, with line
mapping). It answers only the coarse questions Phase 0 needs: which post-image
files does a diff touch, which lines were added to which file, and how big is
the change.
"""
from __future__ import annotations

from collections.abc import Iterator


def iter_added_lines(diff_text: str) -> Iterator[tuple[str | None, str]]:
    """Yield (post_image_path, content) for every added line, '+' stripped.

    Path is None for additions before the first file header or to a deleted
    file (whose post-image header is ``+++ /dev/null``).
    """
    current: str | None = None
    for line in diff_text.splitlines():
        if line.startswith("+++ b/"):
            current = line[6:]
        elif line.startswith("+++ "):
            current = None
        elif line.startswith("+") and not line.startswith("+++"):
            yield current, line[1:]


def post_image_paths(diff_text: str) -> list[str]:
    """Every file present after the change (deleted files excluded)."""
    return [l[6:] for l in diff_text.splitlines() if l.startswith("+++ b/")]


def diff_stats(diff_text: str) -> tuple[int, int]:
    """(files_touched, changed_lines): adds + deletes, headers excluded."""
    files = changed = 0
    for line in diff_text.splitlines():
        if line.startswith("diff --git "):
            files += 1
        elif line.startswith("+") and not line.startswith("+++"):
            changed += 1
        elif line.startswith("-") and not line.startswith("---"):
            changed += 1
    return files, changed
