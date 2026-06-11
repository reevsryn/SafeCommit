# Phase 1 notes — detector requirements discovered during Phase 0

A standing file for design requirements that the corpus and benchmark surface
*before* detector work starts. Each entry carries the real case IDs that
motivated it, so the requirement stays falsifiable and doesn't decay into
folklore. Add new entries as the corpus teaches us more.

## R1 — Context-aware suppression: string literals and test-data fixtures
*(discovered 2026-06-11, while QA-verifying the known-good corpus)*

**Requirement.** The Phase 1 classifier (pipeline stage [4]) must not treat
every syntactic import in a changed `.py` file as a candidate. Two contexts
contain imports that are syntactically real but never executed, and both occur
in top-tier real-world repos:

1. **Imports inside string literals** — test fixtures that embed Python source
   as a string (`textwrap.dedent("""...""")`, pytester-style source blocks)
   which the test then writes to disk or feeds to a tool. tree-sitter handles
   this naturally — a string literal is not an `import_statement` node — but
   only if we parse the real file contents; any line/regex-based shortcut
   re-introduces the false positive.
2. **Imports in test-data / fixture files** — whole `.py` files that exist as
   *input data* for a tool (formatter test cases, parser corpora). These parse
   as genuine import statements even under tree-sitter, so AST awareness is
   NOT sufficient; the classifier needs **path-aware suppression** (e.g.
   `tests/data/`, `test_data/`, `fixtures/`, project-specific corpora dirs)
   and/or repo-config allowlisting.

**Evidence (both from the mined known-good corpus):**

- `pypa__pip__pr13912` — `import pip_unexpected_module_xyz` inside a
  `textwrap.dedent` string in `tests/functional/test_install.py`. The module's
  nonexistence is the test's point. Our regex-based corpus verifier (correctly
  humble about this) quarantined it; the owner adjudicated it back in as a
  deliberately-hard noise case. A line-scanning detector false-positives here;
  a parsing detector must not.
- `psf__black__pr5161` — `from m import (...)` in a black formatter test-data
  file. Syntactically a real import even to a parser. It evaded our QA only
  because a PyPI distribution named `m` coincidentally exists — i.e. this
  pattern WILL false-positive on registry lookup the moment a fixture uses a
  placeholder name that isn't coincidentally registered.

**Anti-overreach constraint.** Do **not** suppress all of `tests/`. Imports at
the top of *executed* test files run under pytest and a hallucinated one fails
CI — those are legitimate findings. The boundary is "code that executes" vs.
"code that is data". The step 3 seeded corpus should include a fake import in
an executed test file as a tripwire against overbroad suppression.

**Benchmark implication.** Keep both cases in the known-good set. They are the
noise floor doing its job: they punish naive line-scanning tools and reward the
parse-then-verify architecture — which is the product's whole bet.
