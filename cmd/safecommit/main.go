// Command safecommit scans a unified diff for references to things that do not
// exist — hallucinated imports and fabricated dependencies in Python code.
//
// PHASE 1, STEP 3: parse and extract, with no verification yet. Every added
// import becomes a finding, so the benchmark shows raw candidate volume before
// any suppression exists to mask it. Step 4's resolution cascade must crush
// that number toward zero.
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
  --version           print version and exit
`

var version = "0.0.3-phase1-step3"

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
	fs.SetOutput(io.Discard) // we print our own usage
	diffPath := fs.String("diff", "", "read the diff from this file instead of stdin")
	repoRoot := fs.String("repo-root", ".", "repo checkout for first-party resolution")
	noRepoContext := fs.Bool("no-repo-context", false, "resolve using only the diff")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	diff, err := readDiff(*diffPath)
	if err != nil {
		return err
	}

	root := *repoRoot
	if *noRepoContext {
		root = ""
	}

	findings, err := scan(diff, root)
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

// scan is the detection pipeline. Steps land here in order:
//
//	[1] parse the unified diff into files + added lines with real line numbers
//	[2] parse changed Python with tree-sitter (NOT regex — see PHASE1-NOTES.md R1)
//	[3] extract candidate imports and dependency-manifest entries
//	[4] resolve: relative -> stdlib -> first-party -> alias -> registry
//	[5] confidence gate: fire only on proven non-existence
//	[6] emit
//
// repoRoot is "" when no checkout is available (the benchmark case), in which
// case stages [4] and [5] must work from the diff alone. That path is measured
// to resolve 427 of 428 real candidates correctly, so it is a conservative
// lower bound on production precision rather than a crippled mode.
//
// Step 3 implements [1]-[3]: parse, extract, and emit EVERYTHING extracted.
//
// This is deliberately max-noise — the Go equivalent of tools/dummy/always_fire.py.
// No resolution or suppression exists yet, so every added import becomes a
// finding. The point is to make extraction quality directly visible in the
// benchmark before any suppression logic exists to mask it: noise here is the
// raw candidate volume that step 4's cascade must then crush toward zero.
//
// Relative imports are skipped rather than suppressed: `from . import x` has
// no package name to report, and the harness contract requires a name. That is
// structural, not tuning.
func scan(src []byte, repoRoot string) ([]finding.Finding, error) {
	files, err := diff.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parsing diff: %w", err)
	}

	ex := pyparse.NewExtractor()
	defer ex.Close()

	out := []finding.Finding{}
	for i := range files {
		for _, im := range ex.FromFile(&files[i]) {
			if im.Kind == pyparse.KindRelative {
				continue
			}
			out = append(out, finding.Finding{
				Name:    im.Top,
				File:    im.File,
				Line:    im.Line,
				Kind:    finding.KindImport,
				Message: "extracted import " + im.Module + " (step 3: no verification yet)",
			})
		}
	}
	_ = repoRoot // stages [4]-[5] will use this
	return out, nil
}
