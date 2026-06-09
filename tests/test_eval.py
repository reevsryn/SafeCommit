"""End-to-end tests: run each dummy tool over the fixture corpora through the
real evaluate() pipeline and assert the EXACT headline numbers.

This proves the whole path (subprocess -> parse -> match -> aggregate -> metrics)
produces the precision/recall the math predicts.

Run from the repo root:  python3 -m unittest discover -t . -s tests -v
"""
import sys
import unittest
from pathlib import Path

from bench.runner import evaluate

ROOT = Path(__file__).resolve().parents[1]
KG = ROOT / "fixtures" / "known-good"
SEED = ROOT / "fixtures" / "seeded"


def dummy(name: str) -> list[str]:
    return [sys.executable, str(ROOT / "tools" / "dummy" / name)]


class TestEndToEnd(unittest.TestCase):
    def by_name(self, report):
        return {c.name: c for c in report.corpora}

    def triple(self, c):
        return (c.counts.tp, c.counts.fp, c.counts.fn)

    def test_never_fire(self):
        # 0 noise, but 0 recall: it misses all 3 seeded hallucinations.
        r = evaluate(dummy("never_fire.py"), [KG, SEED])
        self.assertEqual(r.total.errors, [])  # guard: a crash also emits nothing
        self.assertEqual(r.total.num_findings, 0)
        self.assertEqual(r.total.noise, 0)
        self.assertEqual(self.triple(r.total), (0, 0, 3))
        self.assertEqual(r.total.metrics.recall, 0.0)
        self.assertIsNone(r.total.metrics.precision)

    def test_always_fire(self):
        # Max recall, but fires on every import -> heavy noise, weak precision.
        r = evaluate(dummy("always_fire.py"), [KG, SEED])
        self.assertEqual(r.total.errors, [])
        by = self.by_name(r)
        self.assertEqual(by["known-good"].num_findings, 6)
        self.assertEqual(by["known-good"].noise, 6)
        self.assertEqual(self.triple(by["seeded"]), (3, 3, 0))
        self.assertEqual(by["seeded"].metrics.recall, 1.0)
        self.assertAlmostEqual(by["seeded"].metrics.precision, 0.5)
        self.assertEqual(r.total.noise, 6)
        self.assertEqual(self.triple(r.total), (3, 9, 0))
        self.assertAlmostEqual(r.total.metrics.precision, 0.25)
        self.assertEqual(r.total.metrics.recall, 1.0)

    def test_fixed_list(self):
        # The ideal shape: 0 noise, full recall, perfect precision.
        r = evaluate(dummy("fixed_list.py"), [KG, SEED])
        self.assertEqual(r.total.errors, [])
        self.assertEqual(r.total.noise, 0)
        self.assertEqual(self.triple(r.total), (3, 0, 0))
        self.assertEqual(r.total.metrics.precision, 1.0)
        self.assertEqual(r.total.metrics.recall, 1.0)
        self.assertEqual(r.total.metrics.f1, 1.0)


if __name__ == "__main__":
    unittest.main()
