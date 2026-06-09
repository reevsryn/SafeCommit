"""SafeCommit evaluation harness (Phase 0).

A "benchmark" here is nothing magical: a fixed pile of input diffs with known
correct answers, plus a script that scores any tool against them.

The harness never looks *inside* a diff. It treats each diff as an opaque blob,
hands it to whatever tool is under test (subprocess, JSON over stdout), and
compares that tool's findings to the known answers. Because the boundary is just
"feed diff in -> get findings out", the harness can grade anything: a dummy
script today, the real Go `safecommit` binary later, even a competitor via a
small adapter -- with zero changes to the scorer.
"""

__version__ = "0.0.0"
