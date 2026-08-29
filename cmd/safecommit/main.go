// Command safecommit scans a unified diff for references to things that do not
// exist — hallucinated imports and fabricated dependencies in Python code.
//
// PHASE 1, STEP 4: extraction plus the resolution cascade. A finding is emitted
// only when ground truth proves the name absent from the package registry.
// Anything undetermined — an unreachable registry, an offline run, a name we
// cannot map — produces silence, never a finding.
//
// Usage:
//
//	safecommit scan [flags] < some.diff
//
// Exit codes: 0 = no findings, 1 = findings emitted, 2 = usage or internal error.
// The harness grades stdout, not the exit code; CI consumers use the exit code.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/reevsryn/safecommit/internal/diff"
	"github.com/reevsryn/safecommit/internal/finding"
	"github.com/reevsryn/safecommit/internal/pyparse"
	"github.com/reevsryn/safecommit/internal/registry"
	"github.com/reevsryn/safecommit/internal/resolve"
)

const usage = `safecommit — verify that a diff references things that actually exist

usage:
  safecommit scan [flags] < some.diff

flags:
  --diff PATH         read the diff from PATH instead of stdin
  --repo-root PATH    repo checkout to resolve first-party names and read full
                      file contents from (default ".")
  --no-repo-context   ignore --repo-root; resolve using only what the diff
                      itself contains
  --cache-dir PATH    registry lookup cache (default ".safecommit-cache/pypi")
  --offline           never contact the registry; uncached names stay silent
  --explain           write one verdict line per candidate to stderr
  --version           print version and exit
`

var version = "0.0.4-phase1-step4"

type options struct {
	diffPath string
	repoRoot string
	cacheDir string
	offline  bool
	explain  bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "safecommit: %v\n", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
	if len(args) == 0 || args[0] != "scan" {
		return fmt.Errorf("expected subcommand \"scan\"\n\n%s", usage)
	}

	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	diffPath := fs.String("diff", "", "read the diff from this file instead of stdin")
	repoRoot := fs.String("repo-root", ".", "repo checkout for first-party resolution")
	noRepoContext := fs.Bool("no-repo-context", false, "resolve using only the diff")
	cacheDir := fs.String("cache-dir", ".safecommit-cache/pypi", "registry lookup cache")
	offline := fs.Bool("offline", false, "never contact the registry")
	explain := fs.Bool("explain", false, "write one verdict line per candidate to stderr")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	src, err := readDiff(*diffPath)
	if err != nil {
		return err
	}

	opts := options{diffPath: *diffPath, repoRoot: *repoRoot, cacheDir: *cacheDir, offline: *offline, explain: *explain}
	if *noRepoContext {
		opts.repoRoot = ""
	}

	findings, err := scan(src, opts)
	if err != nil {
		return err
	}

	out := bufio.NewWriter(os.Stdout)
	if err := finding.Emit(out, findings); err != nil {
		return fmt.Errorf("writing findings: %w", err)
	}
	if err := out.Flush(); err != nil {
		return fmt.Errorf("flushing stdout: %w", err)
	}

	if len(findings) > 0 {
		os.Exit(1) // CI signal: this diff has findings
	}
	return nil
}

// readDiff returns the diff text from path, or from stdin when path is empty.
func readDiff(path string) ([]byte, error) {
	if path == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading diff from stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading diff: %w", err)
	}
	return b, nil
}

// scan is the detection pipeline:
//
//	[1] parse the unified diff into files + added lines with real line numbers
//	[2] parse changed Python with tree-sitter (NOT regex — PHASE1-NOTES.md R1)
//	[3] extract candidate imports
//	[4] resolve: relative -> stdlib -> first-party -> alias -> registry
//	[5] confidence gate + dependency-manifest parsing        (step 5)
//	[6] emit
//
// Step 4 implements [1]-[4] and [6]. The manifest path ([5], R3) does not
// exist yet, so `requirement`-kind truths are structurally unreachable here —
// that gap is expected and is exactly what R3 exists to keep visible.
func scan(src []byte, opts options) ([]finding.Finding, error) {
	files, err := diff.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parsing diff: %w", err)
	}

	paths := make([]string, 0, len(files))
	for i := range files {
		if files[i].NewPath != "" {
			paths = append(paths, files[i].NewPath)
		}
	}

	res := &resolve.Resolver{
		FirstParty: resolve.FirstPartyFromPaths(paths),
		Registry:   registry.NewPyPI(opts.cacheDir, opts.offline),
	}

	ex := pyparse.NewExtractor()
	defer ex.Close()

	out := []finding.Finding{}
	for i := range files {
		for _, im := range ex.FromFile(&files[i]) {
			d := res.Resolve(im)
			if opts.explain {
				// stderr only: stdout is the harness contract and must stay
				// a clean JSON array.
				fmt.Fprintf(os.Stderr, "%s\t%s\t%s\t%s:%d\t%s\n",
					verdictName(d.Verdict), d.Reason, im.Top, im.File, im.Line, d.Detail)
			}
			if d.Verdict != resolve.Fire {
				continue
			}
			out = append(out, finding.Finding{
				Name: im.Top,
				File: im.File,
				Line: im.Line,
				Kind: finding.KindImport,
				Message: fmt.Sprintf(
					"no PyPI distribution provides the import %q (%s). "+
						"This is the signature of an AI-hallucinated dependency.",
					im.Module, d.Detail),
			})
		}
	}
	return out, nil
}

func verdictName(v resolve.Verdict) string {
	switch v {
	case resolve.Fire:
		return "FIRE"
	case resolve.Suppress:
		return "suppress"
	default:
		return "SILENT"
	}
}
