"""Unit tests for the scoring math, in isolation (no subprocess, no fixtures).

These encode HAND-COMPUTED precision/recall/F1 for known TP/FP/FN inputs. They
are the rigorous proof that the measuring stick is correct.

Run from the repo root:  python3 -m unittest discover -t . -s tests -v
"""
import unittest

from bench.model import Counts, Finding, Truth, metrics_from_counts, normalize_name
from bench.score import aggregate, match_case, parse_findings


class TestNormalize(unittest.TestCase):
    def test_pep503(self):
        self.assertEqual(normalize_name("Foo_Bar"), "foo-bar")
        self.assertEqual(normalize_name("foo.bar"), "foo-bar")
        self.assertEqual(normalize_name("pandas_helpers"), "pandas-helpers")
        self.assertEqual(normalize_name("  Requests  "), "requests")


class TestMetrics(unittest.TestCase):
    def test_perfect(self):
        m = metrics_from_counts(Counts(tp=3, fp=0, fn=0))
        self.assertEqual(m.precision, 1.0)
        self.assertEqual(m.recall, 1.0)
        self.assertEqual(m.f1, 1.0)

    def test_always_fire(self):
        # 3 caught, 3 false alarms, nothing missed -> P=0.5, R=1.0, F1=2/3
        m = metrics_from_counts(Counts(tp=3, fp=3, fn=0))
        self.assertAlmostEqual(m.precision, 0.5)
        self.assertEqual(m.recall, 1.0)
        self.assertAlmostEqual(m.f1, 2 / 3)

    def test_never_fire(self):
        # nothing emitted -> precision undefined; recall 0; F1 undefined (no P)
        m = metrics_from_counts(Counts(tp=0, fp=0, fn=3))
        self.assertIsNone(m.precision)
        self.assertEqual(m.recall, 0.0)
        self.assertIsNone(m.f1)

    def test_all_false_positives(self):
        # known-good noise: everything emitted is wrong, no truths to recall
        m = metrics_from_counts(Counts(tp=0, fp=6, fn=0))
        self.assertEqual(m.precision, 0.0)
        self.assertIsNone(m.recall)
        self.assertIsNone(m.f1)

    def test_fired_but_all_wrong(self):
        # both P and R are defined and zero -> F1 is a real 0.0, not undefined
        m = metrics_from_counts(Counts(tp=0, fp=2, fn=2))
        self.assertEqual(m.precision, 0.0)
        self.assertEqual(m.recall, 0.0)
        self.assertEqual(m.f1, 0.0)

    def test_half(self):
        m = metrics_from_counts(Counts(tp=1, fp=1, fn=2))
        self.assertAlmostEqual(m.precision, 0.5)
        self.assertAlmostEqual(m.recall, 1 / 3)
        self.assertAlmostEqual(m.f1, 0.4)

    def test_empty(self):
        m = metrics_from_counts(Counts())
        self.assertIsNone(m.precision)
        self.assertIsNone(m.recall)
        self.assertIsNone(m.f1)


class TestMatch(unittest.TestCase):
    @staticmethod
    def f(*names):
        return [Finding(name=n) for n in names]

    @staticmethod
    def t(*names):
        return [Truth(name=n) for n in names]

    def triple(self, r):
        return (r.counts.tp, r.counts.fp, r.counts.fn)

    def test_exact(self):
        r = match_case("c", self.f("reqursts"), self.t("reqursts"))
        self.assertEqual(self.triple(r), (1, 0, 0))

    def test_case_insensitive(self):
        r = match_case("c", self.f("Reqursts"), self.t("reqursts"))
        self.assertEqual(self.triple(r), (1, 0, 0))

    def test_pep503_match(self):
        r = match_case("c", self.f("pandas_helpers"), self.t("pandas-helpers"))
        self.assertEqual(self.triple(r), (1, 0, 0))

    def test_tp_and_fp(self):
        r = match_case("c", self.f("requests", "reqursts"), self.t("reqursts"))
        self.assertEqual(self.triple(r), (1, 1, 0))
        self.assertEqual(r.fp_names, ["requests"])

    def test_miss_is_fn(self):
        r = match_case("c", self.f(), self.t("reqursts"))
        self.assertEqual(self.triple(r), (0, 0, 1))

    def test_finding_on_clean_case_is_fp(self):
        r = match_case("c", self.f("requests"), self.t())
        self.assertEqual(self.triple(r), (0, 1, 0))


class TestParse(unittest.TestCase):
    def test_empty(self):
        self.assertEqual(parse_findings(""), [])
        self.assertEqual(parse_findings("  \n "), [])

    def test_valid(self):
        out = parse_findings('[{"name": "x", "file": "a.py", "line": 3}]')
        self.assertEqual(out[0].name, "x")
        self.assertEqual(out[0].file, "a.py")
        self.assertEqual(out[0].line, 3)

    def test_not_an_array(self):
        with self.assertRaises(ValueError):
            parse_findings('{"name": "x"}')

    def test_missing_name(self):
        with self.assertRaises(ValueError):
            parse_findings('[{"file": "a.py"}]')


class TestAggregate(unittest.TestCase):
    def test_sum(self):
        results = [
            match_case("a", [Finding(name="reqursts")], [Truth(name="reqursts")]),
            match_case("b", [Finding(name="requests")], []),  # clean case -> FP
        ]
        c = aggregate(results)
        self.assertEqual((c.tp, c.fp, c.fn), (1, 1, 0))


if __name__ == "__main__":
    unittest.main()
