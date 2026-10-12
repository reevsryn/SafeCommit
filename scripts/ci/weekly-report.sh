#!/usr/bin/env bash
# Open -- or update -- the single tracking issue for a failed weekly check.
#
# Kept as a script rather than inline workflow YAML so it can be exercised from a
# terminal against the real repository before it ever runs unattended. A
# watchdog whose alerting path has never fired is not a watchdog.
#
# Inputs (environment):
#   GH_REPO     owner/name                          (required)
#   RUN_URL     link to the failing workflow run    (required)
#   SEEDED      result of the seeded-names job      success|failure|skipped
#   PYLATEST    result of the latest-python job     success|failure|skipped
#   SIM         "true" to file a clearly-labelled simulated issue
#
# De-duplication: if an open issue with the same title exists, comment on it
# instead of filing another, so a week-after-week failure is one thread and not
# a pile. Simulated issues use a different title so a test can never mask, or
# be mistaken for, a real alert.
set -euo pipefail

: "${GH_REPO:?GH_REPO is required}" "${RUN_URL:?RUN_URL is required}"
SEEDED="${SEEDED:-skipped}"
PYLATEST="${PYLATEST:-skipped}"
SIM="${SIM:-false}"

PREFIX="[weekly-check]"
if [[ "$SIM" == "true" ]]; then
  TITLE="$PREFIX SIMULATED failure (safe to close)"
else
  TITLE="$PREFIX a time-decaying assumption has failed"
fi

body="$(mktemp)"
{
  echo "Run: ${RUN_URL}"
  echo
  if [[ "$SIM" == "true" ]]; then
    echo "**This is a SIMULATED failure** (\`simulate-failure\` was set on a manual run)."
    echo "It exists only to prove the alerting path works. Close it."
    echo
  fi
  if [[ "$SEEDED" == "failure" ]]; then
    echo "### A seeded fake package name is now registered on PyPI"
    echo
    echo "\`python -m bench verify-seeded\` found a name in \`corpus/seeded\` that exists."
    echo "Someone registered it, whether defensively or as a slopsquat. The seeded corpus is"
    echo "public, so this is the risk that publishing it created."
    echo
    echo "- The recall figure in \`BENCHMARK.md\` is **invalid** until the affected case is"
    echo "  re-authored with a fresh verified-absent name (see \`corpus/README.md\`)."
    echo "- Do not treat the registered package as safe to install."
    echo
  fi
  if [[ "$PYLATEST" == "failure" ]]; then
    echo "### The test suite fails on the newest Python"
    echo
    echo "Most likely \`internal/stdlib/modules.txt\` is stale: a new stdlib module would be"
    echo "looked up on PyPI, 404, and reported as a hallucinated dependency -- a false"
    echo "positive against the standard library. \`tests/test_stdlib_list.py\` names the"
    echo "missing modules in the run log. Fix: \`internal/stdlib/gen.md\`."
    echo
    echo "If the failing interpreter is a *prerelease*, treat this as an early warning:"
    echo "alphas can add and rename modules before the release is final."
    echo
  fi
  if [[ "$SIM" != "true" && "$SEEDED" != "failure" && "$PYLATEST" != "failure" ]]; then
    echo "A check failed but neither known failure mode matched. Read the run log."
  fi
} > "$body"

existing="$(gh issue list --state open --limit 100 --json number,title \
  --jq ".[] | select(.title == \"${TITLE}\") | .number" | head -1 || true)"

if [[ -n "$existing" ]]; then
  gh issue comment "$existing" --body-file "$body" >/dev/null
  echo "updated existing issue #${existing}"
else
  url="$(gh issue create --title "$TITLE" --body-file "$body")"
  echo "opened ${url}"
fi
rm -f "$body"
