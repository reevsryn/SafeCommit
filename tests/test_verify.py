"""Unit tests for the verifier's pure parts: extraction + resolution cascade.

No network: PyPI is replaced by a stub oracle.
"""
import unittest

from bench.model import normalize_name
from bench.verify import ALIASES, Candidate, extract_candidates, resolve

DIFF = """\
diff --git a/svc/api.py b/svc/api.py
--- a/svc/api.py
+++ b/svc/api.py
@@ -1,2 +1,8 @@
+import os, sys
+import reqursts
+from . import helpers
+from pkg.sub import thing
 def call():
     pass
diff --git a/requirements.txt b/requirements.txt
--- a/requirements.txt
+++ b/requirements.txt
@@ -1 +1,5 @@
+# pinned deps
+flask>=2.0
+git+https://github.com/x/y.git
+-e .
diff --git a/docs/guide.md b/docs/guide.md
--- a/docs/guide.md
+++ b/docs/guide.md
@@ -1 +1,2 @@
+import not_code
"""


class StubOracle:
    def __init__(self, existing):
        self.existing = {normalize_name(n) for n in existing}
        self.calls = []

    def exists(self, name):
        self.calls.append(name)
        return normalize_name(name) in self.existing, "stub"


def cand(top, kind="import", name=None, file="svc/api.py"):
    return Candidate(name or top, top, file, f"import {top}", kind)


class TestExtract(unittest.TestCase):
    def setUp(self):
        self.cands = extract_candidates(DIFF)
        self.by_kind = {}
        for c in self.cands:
            self.by_kind.setdefault(c.kind, []).append(c)

    def test_comma_imports_split(self):
        tops = [c.top for c in self.by_kind["import"]]
        self.assertIn("os", tops)
        self.assertIn("sys", tops)
        self.assertIn("reqursts", tops)

    def test_relative_marked(self):
        self.assertEqual(len(self.by_kind["relative"]), 1)

    def test_from_import_top_level(self):
        self.assertEqual([c.top for c in self.by_kind["from-import"]], ["pkg"])

    def test_requirement_extracted_with_guards(self):
        reqs = self.by_kind["requirement"]
        self.assertEqual([c.top for c in reqs], ["flask"])  # url/-e/comment skipped

    def test_markdown_ignored(self):
        self.assertTrue(all(c.top != "not_code" for c in self.cands))


class TestResolve(unittest.TestCase):
    def test_stdlib(self):
        status, _ = resolve(cand("os"), set(), StubOracle([]))
        self.assertEqual(status, "stdlib")

    def test_first_party(self):
        status, _ = resolve(cand("pkg"), {"pkg"}, StubOracle([]))
        self.assertEqual(status, "first-party")

    def test_relative(self):
        status, _ = resolve(cand("", kind="relative", name="."), set(), StubOracle([]))
        self.assertEqual(status, "relative")

    def test_alias_resolves_via_distribution(self):
        oracle = StubOracle(["PyYAML"])
        status, evidence = resolve(cand("yaml"), set(), oracle)
        self.assertEqual(status, "pypi-alias")
        self.assertIn("PyYAML", evidence)
        # the oracle was asked about the DISTRIBUTION, not the import name
        self.assertEqual(oracle.calls, ["PyYAML"])

    def test_pypi_hit(self):
        status, _ = resolve(cand("flask", kind="requirement"), set(), StubOracle(["flask"]))
        self.assertEqual(status, "pypi")

    def test_unresolved(self):
        status, _ = resolve(cand("reqursts"), set(), StubOracle([]))
        self.assertEqual(status, "unresolved")

    def test_stdlib_wins_over_pypi(self):
        # 'json' exists on PyPI too; the cascade must classify it stdlib and
        # never hit the registry for it.
        oracle = StubOracle(["json"])
        status, _ = resolve(cand("json"), set(), oracle)
        self.assertEqual(status, "stdlib")
        self.assertEqual(oracle.calls, [])


if __name__ == "__main__":
    unittest.main()
