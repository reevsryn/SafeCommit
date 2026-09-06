"""Guard against silent decay of internal/stdlib/modules.txt.

The Go detector embeds a list of Python standard-library module names and
suppresses them before any registry lookup. The list is generated from real
interpreters, so it goes stale the moment Python ships a release with new
modules -- and a stale list is not a cosmetic problem: an unlisted stdlib
module gets looked up on PyPI, returns 404, and produces a FALSE POSITIVE
against the standard library itself.

This exact decay happened once. The list was generated on Python 3.13.7; by the
time 3.14.4 was installed it was missing 8 modules, including the public
`annotationlib` and `compression`. This test exists so it is caught by the test
suite rather than by a user.

Run under every interpreter you care about; each one checks itself.
"""
from __future__ import annotations

import pathlib
import sys
import unittest

REPO = pathlib.Path(__file__).resolve().parent.parent
MODULES = REPO / "internal" / "stdlib" / "modules.txt"


def embedded_modules() -> set[str]:
    out = set()
    for line in MODULES.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            out.add(line)
    return out


class TestStdlibList(unittest.TestCase):
    def test_covers_this_interpreter(self):
        """Every stdlib module of the RUNNING Python must be in the list."""
        embedded = embedded_modules()
        current = set(sys.stdlib_module_names) | set(sys.builtin_module_names)
        missing = sorted(current - embedded)
        v = f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}"
        self.assertEqual(
            missing,
            [],
            f"internal/stdlib/modules.txt is stale for Python {v}: {len(missing)} "
            f"module(s) missing {missing}. Each is a false positive waiting to "
            f"happen against the standard library. Regenerate additively -- see "
            f"internal/stdlib/gen.md.",
        )

    def test_is_additive_only(self):
        """Historical modules must never be dropped by a regeneration."""
        embedded = embedded_modules()
        for m in ("distutils", "imp", "telnetlib", "asynchat", "cgi"):
            self.assertIn(
                m, embedded,
                f"{m!r} was removed from the list; regeneration must be additive, "
                f"because legacy code still imports it and it is not a hallucination.",
            )

    def test_not_truncated(self):
        self.assertGreater(len(embedded_modules()), 280)


if __name__ == "__main__":
    unittest.main()
