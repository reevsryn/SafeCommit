#!/usr/bin/env bash
# Scan a pull request and post a single, updatable comment.
#
# Design notes worth knowing before changing this:
#
#  * The DIFF comes from `gh pr diff`, not from git. A default actions/checkout
#    is shallow, so `git diff base...head` needs extra fetching and gets the
#    merge-base subtly wrong in edge cases. The API always returns exactly the
#    diff GitHub itself shows.
#
#  * The CHECKOUT is still required, and is passed as --repo-root. That is what
#    resolves first-party imports, and it is the mode the published noise figure
#    was measured in (PHASE1-NOTES.md R7). Without it the tool runs degraded and
#    reports first-party modules as hallucinations.
#
#  * SILENCE IS THE PRODUCT. With no findings we post nothing, and delete any
#    comment left by an earlier run, so a fixed PR does not keep a stale
#    warning attached to it.
set -euo pipefail

BIN="${SAFECOMMIT_BIN:?safecommit binary path not set}"
MARKER='<!-- safecommit:findings -->'
EVENT="${GITHUB_EVENT_PATH:-}"

if [[ -z "$EVENT" || ! -f "$EVENT" ]]; then
  echo "::error::no event payload; SafeCommit runs on pull_request events"
  exit 1
fi

PR=$(jq -r '.pull_request.number // empty' "$EVENT")
HEAD_SHA=$(jq -r '.pull_request.head.sha // empty' "$EVENT")
if [[ -z "$PR" ]]; then
  echo "::notice::not a pull_request event; nothing to scan"
  echo "findings=0" >> "$GITHUB_OUTPUT"
  exit 0
fi

REPO="${GITHUB_REPOSITORY}"
LINK_BASE="${GITHUB_SERVER_URL:-https://github.com}/${REPO}/blob/${HEAD_SHA}/"

echo "::group::SafeCommit scan"
gh pr diff "$PR" --repo "$REPO" > "${RUNNER_TEMP}/pr.diff"
echo "diff: $(wc -l < "${RUNNER_TEMP}/pr.diff") lines"

# JSON first: it is the machine-readable result and gives us an exact count.
set +e
"$BIN" scan --repo-root . --diff "${RUNNER_TEMP}/pr.diff" > "${RUNNER_TEMP}/findings.json"
scan_status=$?
set -e
if [[ $scan_status -gt 1 ]]; then
  echo "::error::safecommit failed (exit $scan_status)"
  exit "$scan_status"
fi
COUNT=$(jq 'length' "${RUNNER_TEMP}/findings.json")
echo "findings: $COUNT"
echo "findings=$COUNT" >> "$GITHUB_OUTPUT"

"$BIN" scan --repo-root . --diff "${RUNNER_TEMP}/pr.diff" \
  --format markdown --link-base "$LINK_BASE" > "${RUNNER_TEMP}/comment.md" || true
echo "::endgroup::"

# Locate a comment from a previous run so re-pushes update rather than pile up.
existing=""
if [[ "${SAFECOMMIT_COMMENT}" == "true" ]]; then
  existing=$(gh api "repos/${REPO}/issues/${PR}/comments" --paginate \
    --jq "[.[] | select(.body | contains(\"${MARKER}\")) | .id] | first // empty" 2>/dev/null || true)
fi

if [[ "${SAFECOMMIT_COMMENT}" == "true" ]]; then
  if [[ "$COUNT" -gt 0 ]]; then
    if [[ -n "$existing" ]]; then
      gh api -X PATCH "repos/${REPO}/issues/comments/${existing}" \
        -F body=@"${RUNNER_TEMP}/comment.md" --silent
      echo "updated comment $existing"
    else
      gh api -X POST "repos/${REPO}/issues/${PR}/comments" \
        -F body=@"${RUNNER_TEMP}/comment.md" --silent
      echo "posted a new comment"
    fi
  elif [[ -n "$existing" ]]; then
    # Findings were fixed: remove the stale warning rather than leaving it.
    gh api -X DELETE "repos/${REPO}/issues/comments/${existing}" --silent
    echo "removed the stale comment; nothing to report"
  else
    echo "nothing to report, and nothing to clean up"
  fi
fi

if [[ "$COUNT" -gt 0 ]]; then
  jq -r '.[] | "::error file=\(.file),line=\(.line)::\(.name): \(.message)"' \
    "${RUNNER_TEMP}/findings.json"
  if [[ "${SAFECOMMIT_FAIL}" == "true" ]]; then
    exit 1
  fi
fi
exit 0
