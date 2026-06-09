# SafeCommit — Staged Build Plan

> An AI-aware code reviewer that catches the mistakes AI coding assistants actually make.
> Wedge: **verification, not vibes** — fire a finding only when ground truth proves the code
> references something that does not exist. Built for solo devs / small teams as a GitHub
> Action + CLI, tuned for near-zero false positives.

---

## Context — why this, why now, what we're building

AI coding assistants ship code that is *syntactically perfect but factually wrong*. The two
best-evidenced failure modes:

- **Package hallucination.** Spracklen et al., USENIX Security 2025: across 576k samples / 16
  LLMs, **19.7% of recommended packages did not exist** (commercial ~5.2%, OSS ~21.7%), and
  **43% of hallucinated names recurred on every one of 10 reruns** — i.e. hallucinations are
  *reproducible*, which is what makes slopsquatting possible and detection tractable.
  ([paper](https://arxiv.org/abs/2406.10279), [USENIX](https://www.usenix.org/publications/loginonline/we-have-package-you-comprehensive-analysis-package-hallucinations-code))
- **Insecure code.** Veracode 2025: **45% of AI-generated code introduced a security flaw**,
  with no improvement across model generations. ([Veracode](https://www.veracode.com/blog/genai-code-security-report/))

Existing scanners were built for human-written code and are noisy (SAST runs 10–50% false
positives; AI PR reviewers are "nitpicky"/"talkative"). SafeCommit's bet: the *most verifiable*
AI failures can be checked against ground truth (registries, stdlib, the repo itself) and
reported with near-zero noise — something incumbents either don't do or bury.

**Decisions locked with the owner (2026-06-07):**
1. **Target language for v1 = Python only.** (Engine is still written in Go.) Rationale: the
   wedge only has teeth in *dynamic* languages — Go/Rust/Java/TS compilers already reject
   hallucinated symbols/imports at build time, so SafeCommit adds ~0 value there. Python has
   the most hallucination data, the biggest AI-coding surface, and the cleanest ground truth
   (PyPI + typeshed + stdlib).
2. **Phase 1 beachhead = dependency/import existence.** Cheapest end-to-end slice, ~0 FP by
   construction. The defensible moat (symbol-level) lands in Phase 3.
3. **Deterministic, local core; LLM is an optional explainer only.** No source code leaves the
   machine by default. An LLM (local model or a zero-data-retention API) may *phrase/rank*
   already-verified findings, opt-in. It never decides whether something exists.

---

## Part 1 — Validation & differentiation

### Competitor map

| Tool / class | What it does | Where it's weak (sourced) | The gap SafeCommit exploits |
|---|---|---|---|
| **Socket.dev** | Supply-chain: malicious / typosquat / **slopsquat** package detection, behavioral analysis, reachability, PR comments, free tier ([blog](https://socket.dev/blog/slopsquatting-how-ai-hallucinations-are-fueling-a-new-class-of-supply-chain-attacks), [pricing](https://socket.dev/pricing)) | Reputation-first: it asks "is this *existing* package dangerous?" Strong and well-funded. | Socket owns "is this dependency malicious." It does **not** verify symbols *inside* code (`x.method()` that doesn't exist). That's our moat, not theirs. |
| **Snyk / Semgrep (SAST)** | Pattern / taint static analysis | Industry-wide **10–50% FP**; Snyk frequently "too noisy"; Semgrep now racing to AI-assisted FP suppression ([Konvu](https://konvu.com/compare/snyk-vs-semgrep), [Semgrep](https://semgrep.dev/blog/2025/making-zero-false-positive-sast-a-reality-with-ai-powered-memory/)) | They don't check existence of symbols/imports at all. |
| **AI PR reviewers** (CodeRabbit, Greptile, Qodo/PR-Agent, Cursor BugBot, Graphite Diamond) | An LLM *judges* the diff | The noise complaint, concentrated: Greptile = highest catch **and** highest FP / "nitpicks"; CodeRabbit "most talkative"; Diamond "missed most critical bugs" ([state of 2025](https://www.devtoolsacademy.com/blog/state-of-ai-code-review-tools-2025/), [11 bots tested](https://www.indiehackers.com/post/we-ran-11-ai-pr-bots-and-kept-1-061e5ec680)) | They **vibe, they don't verify**, and they ship your code to an LLM. We verify deterministically and locally. |
| **Academia (early 2026)** | Deterministic AST + library introspection → hallucinated APIs at **~100% precision, ~88% recall** on Python ([Hallucination Inspector](https://arxiv.org/abs/2604.20202), [deterministic AST](https://arxiv.org/html/2601.19106v1), [empirical](https://arxiv.org/html/2604.07755v1)) | **Technique proven, unproductized.** No frictionless Action/CLI a stranger can adopt; no PR integration; no benchmark vs. incumbents. |

### Is the thesis still open? — honest read
- **Partly occupied, not closed.** The dependency-*reputation* angle is Socket's; the
  general LLM-judge review angle is a crowded, consolidating market (Graphite was acquired by
  Cursor in Dec 2025). The *deterministic symbol-existence* angle in dynamic languages is
  proven in research but **not shipped as a low-noise PR tool** — that's the opening.
- **The technique is not the novelty; the product is.** Academia already gets ~100% precision
  on Python hallucinated APIs. Our contribution is productization: diff-native, zero-config,
  near-zero-FP, privacy-first, CI-integrated, with an explanation a reviewer trusts.

### The single tightest v1 wedge
> **"SafeCommit verifies that the code references things that actually exist."**
> v1 beachhead: **hallucinated imports / fabricated dependencies in Python**, checked against
> the package registry as ground truth. Moat extension (Phase 3): **hallucinated symbols**
> (methods/attributes/functions/signatures) in Python, checked against stdlib + typeshed +
> the repo itself. Both are ground-truth-verifiable → both can hit near-zero FP. We
> deliberately do **not** chase "plausible-but-wrong logic" in v1 — it requires judgment, not
> verification, and would blow the FP budget.

---

## Part 2 — Scope

### In v1
- A **Go CLI** (`safecommit scan`) and a **GitHub Action** wrapping it.
- **Python target only.** Detect (a) imports of packages that don't exist on PyPI, and (b)
  [Phase 3] calls to symbols that don't exist in stdlib/typeshed/the repo.
- **Deterministic, local** detection. Optional LLM *explainer* behind a flag.
- A **single high-signal PR review comment** (or clean exit when nothing fires).
- A **reproducible benchmark** and a **landing page with before/after evidence**.

### NOT in v1 (scope discipline)
- ❌ Other target languages (Go/JS/TS/Java/Rust). Compilers/`tsc` already catch this there.
- ❌ "Plausible-but-wrong logic" / general bug-finding (subjective → noisy).
- ❌ LLM-in-the-detection-loop (reintroduces FP + privacy cost we're differentiating from).
- ❌ SaaS backend, accounts/teams/dashboard, auto-fix/PR-suggestions, IDE plugin.
- ❌ Non-GitHub SCMs (GitLab/Bitbucket).
- ❌ Reachability/exploitability scoring (that's Socket/Snyk's lane).

### "Low noise" defined measurably
- **Precision = TP / (TP + FP)** over findings emitted.
  - **Dependency check (Phase 1): target ≥ 99% precision** (it's ground truth; the only FP
    sources are misclassified local/private/stdlib imports — all suppressible).
  - **Symbol check (Phase 3): target ≥ 95% precision**, recall reported honestly (academia's
    ~88% recall is the reference, not a promise).
- **Headline benchmark metric:** on a corpus of **real, already-merged Python PRs**
  (known-good), SafeCommit fires **~0 times** (noise floor), while catching seeded
  hallucinations in a separate set. See Part 5.

---

## Part 3 — Technical architecture (the parts to *own*, not treat as magic)

### Stack & justification
- **Engine: Go.** Your strength, and the right tool: compiles to a **single static binary** →
  trivial install and a fast, dependency-free GitHub Action (a huge adoption lever). Excellent
  concurrency for fanning out registry lookups.
- **Diff parsing:** parse git's **unified diff** format ourselves (small, well-specified) to
  get changed files + added line ranges.
- **Code parsing:** **tree-sitter** via Go bindings ([go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter)
  / [smacker](https://github.com/smacker/go-tree-sitter)) with the **Python grammar**. Fast,
  incremental, error-tolerant (parses partial/changed files).
- **Ground-truth oracles:**
  - Package existence: **[deps.dev API](https://docs.deps.dev/api/v3/)** (`GET /v3/systems/PYPI/packages/{name}` → exists + versions; **no API key**, covers PyPI/npm/Go/etc.), with the **[PyPI JSON API](https://docs.pypi.org/api/json/)** (`/pypi/{name}/json`, 404 = doesn't exist) as a confirm/fallback. Aggressively cached.
  - Symbol existence (Phase 3): the running **stdlib**, **typeshed** stubs, the repo's own
    parsed symbols, and optionally the project's installed venv / **pyright** as a subprocess.
- **Optional LLM explainer (late, opt-in):** local model or a **[zero-data-retention](https://privacy.claude.com/en/articles/8956058)** API; only writes prose for findings already proven by the deterministic core.

### The detection pipeline (one pass)
```
git diff ──▶ [1] parse unified diff ──▶ added hunks per file
                                          │
            [2] tree-sitter parse Python ─┘──▶ AST of changed files
                                          │
            [3] extract candidates ───────┘──▶ imports + (Phase 3) symbol refs
                                          │
            [4] classify & suppress ──────┘──▶ drop local/stdlib/relative/private
                                          │
            [5] verify vs ground truth ───┘──▶ deps.dev / typeshed (cached)
                                          │
            [6] confidence-gate + report ─┘──▶ 0..n findings → CLI / PR comment
                                          │
            [7] (opt) LLM explainer ──────┘──▶ prose only, never decides existence
```

### The core design tension: static vs. LLM vs. hybrid — resolved
- **LLM-judge (what CodeRabbit/Greptile do):** high recall, *irreducible* FP floor (it's
  guessing), and it must see your code. Rejected as the core.
- **Pure static/deterministic (what we do):** the finding *is* the verification — "package
  `faiss_gpu_utils` returns 404 on PyPI" is a fact, not an opinion. FP only from
  misclassification, which we fix with suppression rules, not probability.
- **Our hybrid is asymmetric:** deterministic core *decides*; an optional LLM only *explains*.
  This keeps near-zero FP and "AI-aware ≠ uses-an-LLM."

> **Demystify:** parsing ≠ resolution. tree-sitter tells you *"this is a call expression named
> `get_json` on `requests`"* — it has **no idea** whether that method exists. Existence is a
> *separate* lookup step (registry / typeshed / repo). Conflating the two is the classic trap;
> keep them as distinct stages [3] and [5].

### Where near-zero FP is actually won (the FP sources to suppress)
- **Phase 1 (deps):** first-party/local modules (resolve against repo files first), relative
  imports (`from . import x`), the **stdlib list**, private/internal registries (config
  allowlist; treat "absent from public registry but plausibly private" as *low confidence →
  suppress*), monorepo workspaces, optional deps in `try/except ImportError`, dev vs runtime
  deps. **This classification work is the product**, not the HTTP call.
- **Phase 3 (symbols):** dynamic attributes (`__getattr__`, `setattr`, monkeypatching),
  re-exports / `__all__` / star imports, version skew (method exists in installed but not
  pinned version), C-extension modules lacking stubs. Fire **only** on high-confidence
  non-existence; when unsure, stay silent.

### Privacy / data model (first-class — your domain, so go deep)
- **Default mode = fully local.** Inputs: the git diff + the repo on disk. The **only** network
  calls are to **public package registries** (deps.dev / PyPI) and they send **package names
  only** — never your source, never your diff. Document this as a guarantee.
  - Even package names can leak intent → offer `--offline` (use a cached/local registry
    snapshot) and a **local cache** so repeat scans make zero network calls.
- **Nothing is persisted server-side; there is no SafeCommit server.** The Action runs on the
  user's own GitHub runner with the repo-scoped `GITHUB_TOKEN`.
- **Optional LLM explainer is the only path where code excerpts could leave the machine** — so
  it's **off by default**, clearly labeled, supports a **local model**, and documents
  [ZDR](https://platform.claude.com/docs/en/manage-claude/api-and-data-retention) for the
  hosted option. Even then, send the *minimal* finding context, not whole files.
- **Threat-model doc** ("what leaves your machine, when, and why") shipped in the README — this
  is how a security-conscious dev is earned, and it's a genuine differentiator vs. every
  LLM-judge reviewer that uploads your diff.

### The 2–3 riskiest technical assumptions, and how Phase 1 de-risks each
1. **"Near-zero FP is achievable on real diffs."** *The* product claim. De-risked Phase 0→1 by
   building the benchmark *first* and choosing the most verifiable check (registry existence,
   ~0 FP by construction), measured on a real known-good PR corpus.
2. **"There's real value beyond just running pyright/mypy/CI."** Honest risk: typed projects
   with good CI gain little. De-risked by targeting the *pre-merge PR moment*, *zero-config on
   a bare diff (no project env needed for deps)*, *registry awareness (catches names CI would
   only fail on at install)*, and benchmarking explicitly against "what the type-checker would
   have caught."
3. **"Symbol resolution without the project's full environment is tractable."** Real and hard
   (Python's import system is dynamic). **Deliberately deferred to Phase 3**, not Phase 1 —
   Phase 1's dependency check needs no environment at all. Phase 3 carries an open decision:
   reimplement resolution in Go vs. shell out to pyright (accuracy vs. single-binary purity).

---

## Part 4 — Staged milestones (riskiest assumption first; each ends demoable)

### Phase 0 — Eval harness & corpus *first* (~½–1 wk)
Build the measuring stick before the detector, because the entire thesis is "low noise," which
is unprovable without it.
- **Deliverable:** a repo + script that (a) mines **50–100 real merged Python PRs** from
  popular repos as the **known-good "noise" set**, and (b) contains **~20 hand-authored
  seeded-hallucination diffs** (fake imports now; fake symbols later) as the **recall set**.
  A `bench` command that runs any tool and reports precision/recall/noise.
- **Concepts to learn:** precision/recall/F1, dataset curation & leakage, mining PRs via the
  GitHub API, why you must **regenerate your own** hallucination set (Spracklen's 205k-name
  master list is **deliberately not public**; only the [methodology/repo](https://github.com/Spracks/PackageHallucination) is).
- **Demystify:** "benchmark" = a fixed input corpus + a scoring script. Nothing more.

### Phase 1 — Thinnest end-to-end slice: dependency hallucination, one repo, ~0 FP (~2–3 wks)
- **Deliverable:** `safecommit scan <diff>` that parses a unified diff, tree-sitter-parses
  changed Python, extracts added imports + dependency declarations (`requirements.txt`,
  `pyproject.toml`, `setup.py`), classifies/suppresses (local/stdlib/relative/private), and
  verifies survivors against deps.dev — firing **only** on non-existent packages with a
  "likely AI hallucination; did you mean …?" explanation. **Proof:** catches one real
  AI-introduced fake import on a real repo with **0 findings on the Phase 0 known-good set**.
- **Concepts to learn:** unified diff format (hunk headers, `@@ -a,b +c,d @@`, line mapping),
  tree-sitter grammars & S-expression queries, Python's import grammar, registry APIs +
  caching/rate-limit etiquette, exit codes.
- **Demystify:** the registry call is just cached HTTP; the "intelligence" is the
  classification in stage [4]. The stdlib check is a static list, not introspection.

### Phase 2 — GitHub Action + PR comment (~2–3 wks)
- **Deliverable:** a published Action (`uses: <you>/safecommit-action@v1`) that runs on
  `pull_request`, scans the PR diff, and posts **one** review comment (or none). Single static
  binary, no install step. Dogfood on your own repos.
- **Concepts to learn:** GitHub Actions model (runners, `GITHUB_TOKEN`, permissions), PR diff
  retrieval, the Reviews/Comments REST+GraphQL API, idempotent comment updates (don't spam on
  re-runs), `action.yml` packaging.
- **Demystify:** an Action = your binary running on GitHub's VM with a scoped token; the comment
  is one authenticated API call.

### Phase 3 — The moat: symbol-level hallucination in Python (~2–3 wks)
- **Deliverable:** detect calls/attributes/imports-from that don't exist in stdlib + typeshed +
  the repo's own symbols (e.g. `requests.get_json()`), with conservative confidence gating.
  This is the genuinely defensible, under-served capability.
- **Open decision (resolve at phase start):** Go-native resolution (single binary, more work,
  partial) **vs.** shell out to **pyright** (accurate, bundles typeshed, but adds a Node/Python
  dependency and breaks single-binary purity). Recommendation leans pyright-as-subprocess
  behind an interface, with a pure-Go fast path for stdlib/repo symbols.
- **Concepts to learn:** name resolution & scopes, **typeshed** & type stubs, Python's dynamic
  attribute model (why this is hard), pyright internals/CLI.
- **Demystify:** "does this method exist" has no single oracle in a dynamic language — it's a
  *best-effort* answer across several sources; that's why we gate on confidence and stay silent
  when unsure.

### Phase 3.5 — Optional deterministic security-default detectors (plays to your strength)
- A small set of **pattern-based, low-FP** checks for AI's "weakened security defaults"
  (e.g. `verify=False`, `ssl._create_unverified_context`, disabled auth, debug=True). Pure
  rules, no LLM. Include only if Phase 0–3 land on schedule.

### Phase 4 — Proof & adoption (~1–2 wks)
- **Deliverable:** run the Phase 0 benchmark **vs. ≥1 incumbent** (Semgrep and/or Socket and/or
  an AI reviewer) on the same corpus; publish a results table; landing page with reproducible
  before/after; docs; `brew install` / single-binary release notes.

### Phase 5 — Optional LLM explainer (opt-in, last)
- Local/ZDR model that turns a verified finding into a clearer sentence and ranks multiple
  findings. **Never** decides existence. Off by default.

---

## Part 5 — Proof & adoption

### Benchmark (resume-defensible *and* user-convincing)
- **Two corpora (from Phase 0):**
  1. **Known-good:** 50–100 real merged Python PRs → measures **noise** (SafeCommit should fire
     ~0 times). This is the headline.
  2. **Seeded-hallucination:** ~20+ diffs with *known* fake imports/symbols → measures
     **recall**.
- **Report:** precision, recall, F1, and raw findings count per tool, SafeCommit vs. incumbent,
  on identical inputs. Lead with: *"On N real merged PRs, SafeCommit emitted X findings
  (target ≈0) while catching Y/Z seeded hallucinations."*
- **Integrity rules:** never invent numbers; label "proven" vs. "hoped"; publish the corpus +
  script so anyone can reproduce; regenerate your own hallucination set (don't claim Spracklen's
  private list).

### Minimum for a stranger to adopt
- **Install:** one of — add ~10 lines of `action.yml`, or `brew install safecommit` /
  download one binary. No account, no backend.
- **Docs:** README with the threat-model/privacy guarantee, a 60-second quickstart, a real
  before/after screenshot of a caught hallucination, and the benchmark table.
- **Landing page:** the wedge in one sentence, the before/after evidence, the privacy promise.

### "v1 is done" =
1. A stranger installs (one binary / one Action YAML), points it at a Python PR, and gets a
   correct, near-zero-FP finding on a real hallucinated **import** (Phase 1–2) and **symbol**
   (Phase 3); **and**
2. A public, reproducible benchmark shows **≥99% precision on the dependency check / ≥95% on
   symbols**, ≈0 findings on the known-good PR corpus, vs. ≥1 incumbent; **and**
3. README threat-model + landing page with before/after are live.

---

## Verification (how we'll prove each phase works end-to-end)
- **Phase 0:** run the `bench` script against a trivial dummy tool; confirm precision/recall
  math and corpus loading are correct.
- **Phase 1:** golden-file tests on hand-built diffs (fake import → 1 finding; local/stdlib/
  relative imports → 0 findings); then run on the known-good corpus and confirm **0** findings;
  then run on a seeded diff and confirm the catch.
- **Phase 2:** open a throwaway PR with a fake import on a sandbox repo; confirm exactly one
  correct comment, and that re-running updates rather than duplicates it.
- **Phase 3:** golden-file tests for hallucinated vs. real symbols incl. the hard cases
  (`__getattr__`, re-exports, version skew) → confirm silence on ambiguous cases.
- **Phase 4:** reproduce the published benchmark from a clean checkout.

---

## Open risks / honest caveats (revisit, don't bury)
- **Dependency-existence overlaps Socket** and is "simple" — its job is to prove the pipeline at
  ~0 FP; the *moat* is Phase 3. If Phase 3 slips, v1 is thinner than the pitch.
- **Value vs. pyright/mypy/CI** must be shown empirically (assumption #2), or skeptics dismiss
  it. The benchmark must include a "what the type-checker already catches" column.
- **Symbol resolution in a dynamic language is genuinely hard** (assumption #3); expect to gate
  conservatively and accept lower recall to protect precision.
- **Market is consolidating fast** (Cursor⇄Graphite). Ship the privacy + verification narrative
  as the durable differentiator, not feature parity.

---

## Recommended next action
On approval: scaffold **Phase 0 first** — the eval harness + the two corpora (mine ~50–100 real
merged Python PRs as the noise set; author ~20 seeded-hallucination diffs) — so "low noise" is
measurable from day one, *before* any detector code. Then build the Phase 1 diff→deps.dev
verifier against one real repo.

## Key question (answered 2026-06-07)
*Target language for v1*, which contradicts the owner's Go comfort zone → **Python only**
(engine still in Go). Phase 1 beachhead → **dependency existence**. LLM posture →
**deterministic core, optional explainer**. Wedge is locked; ready to build Phase 0.
