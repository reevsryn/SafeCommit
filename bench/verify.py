"""QA-verify a mined corpus. This checks the MINING -- it is NOT the
cleanliness guarantee.

FRAMING (owner decision, 2026-06-10): the known-good corpus's cleanliness
rests on SURVIVORSHIP -- every case is a PR that was human-reviewed and merged
into a major project whose CI installs the package and imports it. A
hallucinated import cannot survive that, independent of anything checked here.

This pass re-derives every added import/requirement from the mined diffs and
resolves each name (relative -> stdlib -> first-party -> PyPI). Its job is to
catch corpus CONTAMINATION and MINING/EXTRACTION BUGS (mangled diffs, regex
misfires, genuinely weird entries). It must never be cited as proof the corpus
is clean, because its registry oracle (PyPI existence) is the same oracle the
Phase 1 detector uses: certifying the corpus with the detector's own test
would make a 0-noise benchmark result partly circular. Survivorship is
independent of that oracle; that independence is the point.

Anything unresolved QUARANTINES its case to a review corpus, with a diagnosis
of signals INDEPENDENT of the PyPI oracle (does the name exist in the repo
tree? is it edit-distance-close to a real package? embedded test-fixture
source? optional guarded import?) so a human can judge contamination vs.
verifier blind spot. Nothing is silently kept; nothing is silently dropped.

Scope honesty:
  * Candidates come from REGEXES over added diff lines, not a parser. A line
    inside a docstring or a test's source-code string literal looks identical
    to a real import here (Phase 1's tree-sitter would see the difference).
    The diagnosis surfaces this via path/context signals.
  * Only requirements*.txt names are registry-checked among manifests (they
    are distribution names verbatim). pyproject/setup.py additions gate mining
    inclusion but are not name-checked here: regex-parsing TOML/Python is
    itself a bug source, and CI `pip install` survivorship covers them.
"""
from __future__ import annotations

import argparse
import difflib
import json
import re
import shutil
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import date
from pathlib import Path

from .corpus import Case, load_corpus
from .diffscan import iter_added_lines, post_image_paths
from .mine import API, USER_AGENT, _get_json, _token
from .model import normalize_name

STDLIB = set(sys.stdlib_module_names) | {"__future__", "__main__"}

# Import name -> PyPI distribution name, for the famous mismatches. The two
# are different namespaces; a naive lookup would false-alarm on e.g. `yaml`.
ALIASES = {
    "yaml": "PyYAML",
    "PIL": "Pillow",
    "cv2": "opencv-python",
    "bs4": "beautifulsoup4",
    "dateutil": "python-dateutil",
    "sklearn": "scikit-learn",
    "attr": "attrs",
    "jwt": "PyJWT",
    "dotenv": "python-dotenv",
    "OpenSSL": "pyOpenSSL",
    "Crypto": "pycryptodome",
    "git": "GitPython",
    "pkg_resources": "setuptools",
    "_pytest": "pytest",
}

# Small list of very popular packages, used only for the typo-distance signal.
POPULAR = [
    "requests", "numpy", "pandas", "scipy", "matplotlib", "flask", "django",
    "pytest", "setuptools", "pillow", "aiohttp", "httpx", "urllib3", "certifi",
    "click", "rich", "pydantic", "sqlalchemy", "boto3", "beautifulsoup4",
    "lxml", "cryptography", "packaging", "wheel", "virtualenv", "tox", "mypy",
    "black", "isort", "coverage",
]

# Known import names that the repo-root listing cannot reveal.
SEED_FIRST_PARTY = {
    "scikit-learn/scikit-learn": {"sklearn"},
    "pytest-dev/pytest": {"pytest", "_pytest"},
    "pypa/pip": {"pip"},
}

IMPORT_RE = re.compile(r"^\s*import\s+([A-Za-z_][\w.]*(?:\s*,\s*[A-Za-z_][\w.]*)*)")
FROM_RE = re.compile(r"^\s*from\s+([A-Za-z_.][\w.]*)\s+import\b")
REQ_NAME_RE = re.compile(r"^\s*([A-Za-z0-9][A-Za-z0-9._-]*)")


@dataclass(frozen=True)
class Candidate:
    name: str  # as written in the diff
    top: str   # top-level module name / normalized requirement name
    file: str
    line: str  # raw added-line content, stripped
    kind: str  # "import" | "from-import" | "relative" | "requirement"


def extract_candidates(diff_text: str) -> list[Candidate]:
    out: list[Candidate] = []
    for path, content in iter_added_lines(diff_text):
        if path is None:
            continue
        if path.endswith(".py"):
            m = IMPORT_RE.match(content)
            if m:
                for mod in re.split(r"\s*,\s*", m.group(1)):
                    out.append(
                        Candidate(mod, mod.split(".")[0], path, content.strip(), "import")
                    )
                continue
            m = FROM_RE.match(content)
            if m:
                mod = m.group(1)
                if mod.startswith("."):
                    out.append(Candidate(mod, "", path, content.strip(), "relative"))
                else:
                    out.append(
                        Candidate(mod, mod.split(".")[0], path, content.strip(), "from-import")
                    )
        elif re.search(r"(?:^|/)requirements[^/]*\.txt$", path):
            s = content.strip()
            if not s or s.startswith(("#", "-")) or "://" in s or " @ " in s:
                continue
            m = REQ_NAME_RE.match(s)
            if m:
                out.append(
                    Candidate(m.group(1), normalize_name(m.group(1)), path, s, "requirement")
                )
    return out


class PyPIOracle:
    """Cached PyPI existence lookups: 200 = exists, 404 = does not.

    Cache lives under .bench-cache/ so reruns make zero network calls.
    """

    def __init__(self, cache_dir: Path, sleep: float = 0.2):
        self.cache_dir = cache_dir
        self.sleep = sleep
        cache_dir.mkdir(parents=True, exist_ok=True)

    def exists(self, name: str, refresh: bool = False) -> tuple[bool, str]:
        """refresh=True bypasses cache reads (verify-seeded distrusts stale 404s)."""
        norm = normalize_name(name)
        cache = self.cache_dir / f"{norm}.json"
        if cache.exists() and not refresh:
            d = json.loads(cache.read_text(encoding="utf-8"))
            return d["exists"], f"PyPI {d['status']} (cached {d['checked']})"
        url = f"https://pypi.org/pypi/{urllib.parse.quote(norm)}/json"
        req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                status = resp.status
        except urllib.error.HTTPError as exc:
            if exc.code != 404:
                raise
            status = 404
        time.sleep(self.sleep)
        found = status == 200
        today = date.today().isoformat()
        cache.write_text(
            json.dumps({"name": norm, "status": status, "exists": found, "checked": today}),
            encoding="utf-8",
        )
        return found, f"PyPI {status} (checked {today})"


def resolve(cand: Candidate, fp_names: set[str], oracle) -> tuple[str, str]:
    """Resolve one candidate through the cascade -> (status, evidence)."""
    if cand.kind == "relative":
        return "relative", "relative import: first-party by construction"
    if cand.kind == "requirement":
        ok, ev = oracle.exists(cand.top)
        return ("pypi", ev) if ok else ("unresolved", ev)
    if cand.top in STDLIB:
        return "stdlib", "sys.stdlib_module_names"
    if cand.top in fp_names:
        return "first-party", "repo paths / root listing / seed map"
    if cand.top in ALIASES:
        dist = ALIASES[cand.top]
        ok, ev = oracle.exists(dist)
        if ok:
            return "pypi-alias", f"import alias -> {dist}; {ev}"
        return "unresolved", f"import alias -> {dist}; {ev}"
    ok, ev = oracle.exists(cand.top)
    return ("pypi", ev) if ok else ("unresolved", ev)


# --------------------------------------------------------------------------- #
# First-party discovery & independent diagnosis
# --------------------------------------------------------------------------- #
def _top_component(path: str) -> str:
    parts = path.split("/")
    top = parts[1] if parts[0] == "src" and len(parts) > 1 else parts[0]
    return top[:-3] if top.endswith(".py") else top


def repo_first_party(repo: str, cases: list[Case], token: str | None) -> set[str]:
    """Importable top-level names that are first-party to `repo`."""
    names = set(SEED_FIRST_PARTY.get(repo, set()))
    for case in cases:
        for path in post_image_paths(case.read_diff()):
            top = _top_component(path)
            if top:
                names.add(top)
    try:
        entries = _get_json(f"{API}/repos/{repo}/contents/", token)
        subdirs = []
        for e in entries:
            n = e.get("name", "")
            if e.get("type") == "dir":
                names.add(n)
                if n == "src":
                    subdirs.append(n)
            elif n.endswith(".py"):
                names.add(n[:-3])
        for sub in subdirs:
            for e in _get_json(f"{API}/repos/{repo}/contents/{sub}", token):
                n = e.get("name", "")
                if e.get("type") == "dir":
                    names.add(n)
                elif n.endswith(".py"):
                    names.add(n[:-3])
    except (urllib.error.URLError, urllib.error.HTTPError, RuntimeError) as exc:
        print(f"  warn: root listing failed for {repo}: {exc}", file=sys.stderr)
    names.discard("")
    return names


def _gh_exists(repo: str, path: str, token: str | None) -> bool | None:
    try:
        _get_json(f"{API}/repos/{repo}/contents/{urllib.parse.quote(path)}", token)
        return True
    except urllib.error.HTTPError as exc:
        return False if exc.code == 404 else None
    except (urllib.error.URLError, RuntimeError):
        return None


def diagnose(
    cand: Candidate,
    repo: str,
    diff_text: str,
    resolved_tops: set[str],
    token: str | None,
) -> list[str]:
    """Signals independent of the PyPI oracle, for human adjudication."""
    signals: list[str] = []
    for probe in (cand.top, f"{cand.top}.py", f"src/{cand.top}"):
        if _gh_exists(repo, probe, token):
            signals.append(
                f"'{probe}' EXISTS in the {repo} tree -> likely first-party; "
                "verifier root-scan blind spot, not contamination"
            )
            break
    close = difflib.get_close_matches(
        cand.top.lower(), sorted({*POPULAR, *(t.lower() for t in resolved_tops)}), n=1, cutoff=0.8
    )
    if close:
        signals.append(
            f"edit-distance-close to real package '{close[0]}' -> typo/hallucination "
            "shape (CONTAMINATION signal)"
        )
    if "test" in cand.file.lower():
        signals.append(
            "added in a test file -> may be synthetic example source embedded in a "
            "string literal (regex extractor cannot tell; tree-sitter in Phase 1 would)"
        )
    if re.search(r"except\s+\(?\s*(?:ImportError|ModuleNotFoundError)", diff_text):
        signals.append(
            "diff contains try/except ImportError -> optional-dependency pattern "
            "(name may be intentionally absent)"
        )
    if not signals:
        signals.append(
            "no independent signal either way -> treat as candidate contamination; "
            "human judgment required"
        )
    return signals


# --------------------------------------------------------------------------- #
# Quarantine
# --------------------------------------------------------------------------- #
def quarantine_cases(
    corpus_dir: Path, review_dir: Path, flagged: dict[str, list[dict]]
) -> None:
    """Move flagged cases out of the corpus into the review bucket."""
    manifest = corpus_dir / "manifest.jsonl"
    (review_dir / "diffs").mkdir(parents=True, exist_ok=True)
    kept_lines: list[str] = []
    moved_rows: list[dict] = []
    for raw in manifest.read_text(encoding="utf-8").splitlines():
        s = raw.strip()
        if not s or s.startswith("#"):
            kept_lines.append(raw)
            continue
        row = json.loads(s)
        if row["id"] in flagged:
            row["review_reasons"] = flagged[row["id"]]
            moved_rows.append(row)
        else:
            kept_lines.append(raw)
    with (review_dir / "manifest.jsonl").open("a", encoding="utf-8") as rf:
        for row in moved_rows:
            src = corpus_dir / row["diff"]
            dst = review_dir / "diffs" / src.name
            shutil.move(str(src), str(dst))
            row["diff"] = f"diffs/{src.name}"
            rf.write(json.dumps(row) + "\n")
    manifest.write_text("\n".join(kept_lines) + "\n", encoding="utf-8")


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
def add_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--corpus", default="corpus/known-good", help="corpus dir to verify")
    parser.add_argument(
        "--review-dir", default="corpus/review", help="quarantine destination"
    )
    parser.add_argument(
        "--cache-dir", default=".bench-cache/pypi", help="PyPI response cache"
    )
    parser.add_argument("--sleep", type=float, default=0.2, help="pause between PyPI calls")
    parser.add_argument(
        "--no-quarantine",
        action="store_true",
        help="report only; do not move flagged cases",
    )


def run_cli(args: argparse.Namespace) -> int:
    corpus_dir = Path(args.corpus)
    review_dir = Path(args.review_dir)
    token = _token()
    if token is None:
        print(
            "note: no GITHUB_TOKEN; repo root listings / tree probes run "
            "unauthenticated (fine for a handful of calls)",
            file=sys.stderr,
        )

    cases = load_corpus(corpus_dir)
    by_repo: dict[str, list[Case]] = defaultdict(list)
    for case in cases:
        by_repo[case.source.get("repo", "(unknown)")].append(case)

    print(f"verifying {corpus_dir} — {len(cases)} case(s) across {len(by_repo)} repo(s)")
    print("framing: survivorship is the cleanliness guarantee; this pass QAs the mining\n")

    fp_sets = {
        repo: repo_first_party(repo, repo_cases, token)
        for repo, repo_cases in by_repo.items()
    }
    oracle = PyPIOracle(Path(args.cache_dir), sleep=args.sleep)

    occurrences: Counter = Counter()
    name_table: dict[tuple[str, str, str], dict] = {}
    flagged: dict[str, list[dict]] = defaultdict(list)
    resolved_tops: set[str] = set()

    for repo, repo_cases in by_repo.items():
        for case in repo_cases:
            diff_text = case.read_diff()
            adjudicated_names = {a.get("name") for a in case.adjudicated}
            for cand in extract_candidates(diff_text):
                status, evidence = resolve(cand, fp_sets[repo], oracle)
                if status == "unresolved" and (
                    cand.top in adjudicated_names or cand.name in adjudicated_names
                ):
                    status = "adjudicated-keep"
                    evidence += "; owner adjudication on file in manifest"
                occurrences[status] += 1
                key = (repo, cand.kind, cand.top or cand.name)
                entry = name_table.setdefault(
                    key,
                    {
                        "repo": repo,
                        "name": cand.top or cand.name,
                        "kind": cand.kind,
                        "status": status,
                        "evidence": evidence,
                        "cases": [],
                    },
                )
                if case.id not in entry["cases"]:
                    entry["cases"].append(case.id)
                if status != "unresolved":
                    if cand.top:
                        resolved_tops.add(cand.top)
                else:
                    flagged[case.id].append(
                        {
                            "name": cand.name,
                            "top": cand.top,
                            "file": cand.file,
                            "line": cand.line,
                            "kind": cand.kind,
                            "evidence": evidence,
                            "signals": diagnose(cand, repo, diff_text, resolved_tops, token),
                        }
                    )

    print("candidate occurrences by resolution:")
    for status in (
        "stdlib",
        "first-party",
        "relative",
        "pypi",
        "pypi-alias",
        "adjudicated-keep",
        "unresolved",
    ):
        if occurrences.get(status):
            print(f"  {status:<12}{occurrences[status]:>6}")
    distinct_pypi = sum(
        1 for v in name_table.values() if v["status"] in ("pypi", "pypi-alias")
    )
    print(f"\ndistinct names registry-checked: {distinct_pypi} (cache: {args.cache_dir})")

    if flagged:
        print(f"\nUNRESOLVED -> {len(flagged)} case(s) to quarantine:")
        for case_id, items in flagged.items():
            for it in items:
                print(f"  {case_id} :: '{it['name']}' ({it['file']})")
                print(f"      line: {it['line']}")
                for sig in it["signals"]:
                    print(f"      - {sig}")
    else:
        print("\nno unresolved names — nothing to quarantine")

    report = {
        "generated": date.today().isoformat(),
        "corpus": str(corpus_dir),
        "framing": (
            "Survivorship (merged + CI in a major repo) is the cleanliness "
            "guarantee. This report is QA on the mining. Its PyPI oracle is the "
            "same oracle the Phase 1 detector uses, so it must not be cited as "
            "independent proof of corpus cleanliness."
        ),
        "occurrences": dict(occurrences),
        "names": sorted(name_table.values(), key=lambda v: (v["repo"], v["name"])),
        "unresolved": {cid: items for cid, items in flagged.items()},
    }
    report_path = corpus_dir / "verification-report.json"
    report_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(f"\nreport written to {report_path}")

    if flagged and not args.no_quarantine:
        quarantine_cases(corpus_dir, review_dir, dict(flagged))
        print(f"quarantined {len(flagged)} case(s) to {review_dir}")
    return 0
