// Command safecommit scans a unified diff for references to things that do not
// exist — hallucinated imports and fabricated dependencies in Python code.
//
// PHASE 1, STEP 5: two detection paths — Python imports and dependency
// manifests — behind one resolution cascade. A finding is emitted only when
// ground truth proves the name absent from the package registry. Anything
// undetermined — an unreachable registry, an offline run, a name we cannot map,
// an added line whose enclosing TOML context we cannot establish — produces
// silence, never a finding.
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
	"github.com/reevsryn/safecommit/internal/manifest"
	"github.com/reevsryn/safecommit/internal/pathrules"
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

var version = "0.0.6-phase1-step6"

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
// Step 5 adds the second detection path (dependency manifests, R3) and
// path-aware suppression of fixture data (R1 case 2). The hallucination-signal
// confidence gate is deliberately NOT built: there are zero measured false
// positives to tune against, so it would be speculation at the cost of recall
// on novel hallucinations. Deferred to Phase 4, when the fresh holdout can say
// whether alias gaps (R4) actually produce false positives.
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
		f := &files[i]

		// R1 case 2: whole files that are fixture DATA parse as genuine code.
		// Only the path can tell them apart. Deliberately narrow — see
		// internal/pathrules; suppressing all of tests/ would drop seed-008.
		if pathrules.IsFixtureData(f.NewPath) {
			if opts.explain {
				fmt.Fprintf(os.Stderr, "suppress\tfixture-data\t-\t%s\t-\n", f.NewPath)
			}
			continue
		}

		for _, dep := range manifest.FromFile(f) {
			d := res.ResolveDep(dep.Name)
			if opts.explain {
				fmt.Fprintf(os.Stderr, "%s\t%s\t%s\t%s:%d\t%s\n",
					verdictName(d.Verdict), d.Reason+"/manifest", dep.Name, dep.File, dep.Line, d.Detail)
			}
			if d.Verdict != resolve.Fire {
				continue
			}
			out = append(out, finding.Finding{
				Name: dep.Name,
				File: dep.File,
				Line: dep.Line,
				Kind: finding.KindRequirement,
				Message: fmt.Sprintf(
					"dependency %q does not exist on PyPI (%s). "+
						"This is the signature of a fabricated dependency.",
					dep.Name, d.Detail),
			})
		}

		for _, im := range ex.FromFile(f) {
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
