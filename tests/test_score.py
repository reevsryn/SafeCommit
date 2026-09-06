"""Unit tests for the scoring math, in isolation (no subprocess, no fixtures).

These encode HAND-COMPUTED precision/recall/F1 for known TP/FP/FN inputs. They
are the rigorous proof that the measuring stick is correct.

Run from the repo root:  python3 -m unittest discover -t . -s tests -v
"""
import unittest

from bench.model import Counts, Finding, Truth, metrics_from_counts, normalize_name
from bench.score import aggregate, aggregate_kinds, match_case, parse_findings


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


class TestFileAwareMatching(unittest.TestCase):
    """R2: a finding must agree with the truth about WHERE the problem is.

    The motivating real case is seed-019: the hallucination is a bad pin in
    requirements.txt, while the same name also appears in a perfectly
    legitimate `from dateutil import parser`. Under name-only matching a tool
    that flagged the legitimate import was credited with finding the bug.
    """

    def test_right_name_wrong_file_is_not_a_true_positive(self):
        r = match_case(
            "seed-019",
            [Finding(name="dateutil", file="reports/weekly.py", line=2)],
            [Truth(name="dateutil", file="requirements.txt", kind="requirement")],
        )
        self.assertEqual(r.counts.tp, 0, "wrong-file finding must not earn credit")
        self.assertEqual(r.counts.fn, 1)
        self.assertEqual(r.counts.fp, 1)
        self.assertEqual(r.fn_names, ["dateutil"])

    def test_right_name_right_file_is_a_true_positive(self):
        r = match_case(
            "seed-019",
            [Finding(name="dateutil", file="requirements.txt", line=2)],
            [Truth(name="dateutil", file="requirements.txt", kind="requirement")],
        )
        self.assertEqual((r.counts.tp, r.counts.fp, r.counts.fn), (1, 0, 0))

    def test_finding_without_a_file_cannot_credit_a_located_truth(self):
        r = match_case(
            "c",
            [Finding(name="reqursts")],  # named it, but said nothing about where
            [Truth(name="reqursts", file="svc/api.py", kind="import")],
        )
        self.assertEqual(r.counts.tp, 0)
        self.assertEqual(r.unlocated_names, ["reqursts"],
                         "an unlocated finding should be reported distinctly from a miss")

    def test_truth_without_a_file_matches_on_name_alone(self):
        r = match_case(
            "c",
            [Finding(name="reqursts", file="anywhere.py")],
            [Truth(name="reqursts")],  # no file declared -> nothing to disagree with
        )
        self.assertEqual(r.counts.tp, 1)

    def test_name_mode_restores_the_loose_behaviour(self):
        r = match_case(
            "seed-019",
            [Finding(name="dateutil", file="reports/weekly.py")],
            [Truth(name="dateutil", file="requirements.txt", kind="requirement")],
            match="name",
        )
        self.assertEqual(r.counts.tp, 1, "--match name must reproduce the old scoring")

    def test_per_kind_recall_is_tracked(self):
        r = match_case(
            "mixed",
            [Finding(name="nunpy", file="a.py")],
            [
                Truth(name="nunpy", file="a.py", kind="import"),
                Truth(name="python-requests", file="requirements.txt", kind="requirement"),
            ],
        )
        self.assertEqual(r.kind_hits.get("import"), 1)
        self.assertEqual(r.kind_totals.get("import"), 1)
        self.assertEqual(r.kind_hits.get("requirement", 0), 0)
        self.assertEqual(r.kind_totals.get("requirement"), 1)

    def test_aggregate_kinds_sums_across_cases(self):
        a = match_case("a", [Finding(name="x", file="f.py")],
                       [Truth(name="x", file="f.py", kind="import")])
        b = match_case("b", [], [Truth(name="y", file="requirements.txt", kind="requirement")])
        self.assertEqual(aggregate_kinds([a, b]),
                         {"import": (1, 1), "requirement": (0, 1)})
