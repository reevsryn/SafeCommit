"""TEMPORARY end-to-end check of the SafeCommit Action's finding path.

The import below is deliberately fake. This file exists only to prove the
Action posts a comment, and is removed in the next commit -- which also proves
the Action deletes its own comment once a pull request is fixed.
"""

import json  # stdlib: must be suppressed

import safecommit_demo_not_a_real_package  # deliberately does not exist


def main() -> None:
    print(json.dumps({"ok": safecommit_demo_not_a_real_package.VERSION}))
