# Regenerating `modules.txt`

The list is a **union across Python versions**, not a snapshot of one
interpreter. SafeCommit scans repositories targeting many Python versions, so a
module that is stdlib in *any* supported version must be suppressed.

**Regeneration is additive only. Never drop a name.**

```sh
python3 - <<'EOF' > /tmp/stdlib_new.txt
import subprocess, pathlib

existing = {
    l.strip() for l in pathlib.Path("internal/stdlib/modules.txt").read_text().splitlines()
    if l.strip() and not l.startswith("#")
}
interpreters = ["/usr/local/bin/python3", "/opt/homebrew/bin/python3"]  # add yours
mods, versions = set(existing), []
for exe in interpreters:
    try:
        out = subprocess.run([exe, "-c",
            "import sys;print(sys.version_info.major,sys.version_info.minor,sys.version_info.micro);"
            "print(chr(10).join(sorted(set(sys.stdlib_module_names)|set(sys.builtin_module_names))))"],
            capture_output=True, text=True, check=True).stdout.splitlines()
    except Exception:
        continue
    versions.append(".".join(out[0].split()))
    mods |= {m for m in out[1:] if m.strip()}
mods |= {  # removed from recent versions, still present in real code
    "distutils", "imp", "asynchat", "asyncore", "smtpd", "binhex", "formatter",
    "parser", "symbol", "cgi", "cgitb", "chunk", "crypt", "nis", "nntplib",
    "ossaudiodev", "pipes", "sndhdr", "spwd", "sunau", "telnetlib", "uu",
    "xdrlib", "audioop", "aifc", "mailcap", "msilib", "lib2to3", "typing_extensions",
    "imghdr", "_crypt", "_msi",
    # NEWER than any interpreter installed where this was generated -- added when
    # CI's 3.15 job named them (3.15.0: `profiling` is a public module).
    "profiling", "_math_integer",
}
print("# Python stdlib top-level module names.")
print(f"# Union across: {', '.join(versions)} + previous list + historical set.")
for m in sorted(mods):
    print(m)
EOF
mv /tmp/stdlib_new.txt internal/stdlib/modules.txt
```

## Why this matters more than it looks

An unlisted stdlib module is looked up on PyPI, returns 404, and is reported as
a hallucinated dependency — **a false positive against the standard library**,
which is the exact failure this project exists to avoid.

This decay is not hypothetical. The list was generated on Python 3.13.7. When
3.14.4 arrived it was missing 8 modules, two of them public (`annotationlib`,
`compression`). `tests/test_stdlib_list.py` now fails whenever the list is stale
for the running interpreter — run the Python suite under each Python you
support, and regenerate when it complains.

## The extra set covers interpreters you do not have locally

The union above can only see the Pythons installed on the machine that runs it,
so the hand-maintained set in the recipe exists for everything else, in **both**
directions:

- **Older interpreters (removed modules).** `imghdr`, `_crypt` and `_msi` exist
  in 3.12 and were removed in 3.13. A list generated from 3.13 and 3.14
  silently omitted them, and `imghdr` is a public module real code imports.
- **Newer interpreters (added modules).** `profiling` and `_math_integer` first
  appear in 3.15. A list generated before 3.15 was installed could not know
  them, and `import profiling` would 404 on PyPI and be reported as a
  hallucinated dependency.

Both gaps were found by CI's Python matrix, not by anyone's judgement: each
failed `tests/test_stdlib_list.py` on an interpreter nobody had installed
locally, and the failure message named the missing modules. So the workflow is:
when that test fails, add the named modules to the recipe's extra set above and
to `modules.txt`. `.github/workflows/weekly-checks.yml` also runs the suite on
the newest Python, stable and prerelease, so the next release is noticed within
a week even if nobody pushes a commit.

## Bias on purpose

Prefer a name being *in* this list. A spurious entry costs recall on a package
nobody hallucinates; a missing entry costs a false positive.
