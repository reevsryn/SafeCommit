# Regenerating `modules.txt`

The list is the union of `sys.stdlib_module_names` and
`sys.builtin_module_names` from a real interpreter, plus a hand-maintained set
of modules that *were* stdlib in earlier Python versions and still appear in
real code (`distutils`, `imp`, `asynchat`, `telnetlib`, ...).

```sh
python3 - <<'EOF' > internal/stdlib/modules.txt
import sys
mods = set(sys.stdlib_module_names) | set(sys.builtin_module_names)
historical = {
    "distutils", "imp", "asynchat", "asyncore", "smtpd", "binhex", "formatter",
    "parser", "symbol", "cgi", "cgitb", "chunk", "crypt", "nis", "nntplib",
    "ossaudiodev", "pipes", "sndhdr", "spwd", "sunau", "telnetlib", "uu",
    "xdrlib", "audioop", "aifc", "mailcap", "msilib", "lib2to3", "typing_extensions",
}
mods |= historical
print("# Python stdlib top-level module names.")
print(f"# Generated on Python {sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}")
for m in sorted(m for m in mods if m):
    print(m)
EOF
```

**Bias on purpose.** Prefer a name being *in* this list. A false entry costs
recall on a package nobody hallucinates; a missing entry costs a false
positive, which is the failure mode this project is built to avoid.

**Version skew.** The list is generated from whichever interpreter runs the
command. Scanning a repo on an older Python is fine — the historical set covers
the removals. The reverse (a brand-new stdlib module absent from the list) would
cause one false positive until regenerated; regenerate on each Python release.
