package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/engine/interpreter"
)

// ANSI codes used by testReportStyle. Never used directly — see paint().
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

// isTerminalWriter reports whether out is a real terminal (a character
// device), as opposed to a pipe, redirect, or in-memory buffer — the
// dependency-free heuristic Go CLIs used before golang.org/x/term existed,
// good enough here since mhl has no other reason to depend on it. Honoring
// NO_COLOR (https://no-color.org) as well. Test suites in this package
// write into a bytes.Buffer, which is never an *os.File, so this always
// reports false there — the reason ANSI codes never show up in this
// package's own test assertions no matter how runTests' output is styled.
func isTerminalWriter(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

// testReportStyle is a set of color helpers for runTests' output, all
// no-ops when color is disabled — so the exact PASS/FAIL/SKIP/summary text
// callers (and this package's own tests) match against never changes
// shape, only whether ANSI codes surround it.
type testReportStyle struct{ color bool }

func (s testReportStyle) paint(code, text string) string {
	if !s.color {
		return text
	}
	return code + text + ansiReset
}

func (s testReportStyle) bold(text string) string   { return s.paint(ansiBold, text) }
func (s testReportStyle) green(text string) string  { return s.paint(ansiGreen, text) }
func (s testReportStyle) red(text string) string    { return s.paint(ansiRed, text) }
func (s testReportStyle) yellow(text string) string { return s.paint(ansiYellow, text) }

// statusIcon returns a colored ✓/✗ for a passed/failed count pair — used
// both per-describe and per-test-block, wherever a quick pass/fail glance
// is more useful than reading the numbers.
func (s testReportStyle) statusIcon(failed int) string {
	if failed > 0 {
		return s.red("✗")
	}
	return s.green("✓")
}

// fileReport pairs one .mh file with the test blocks it declared, letting
// the final report's per-suite breakdown and failure list name the file a
// result came from — RunTests itself has no notion of "file", it just
// returns TestResults for whatever program it was handed.
type fileReport struct {
	file    string
	results []*interpreter.TestResult
}

// testFailure is one failed assertion's full address (file, test,
// describe, and the assertion call itself) plus its failure reason —
// enough to jump straight to the offending line without rescrolling
// through the PASS/FAIL log above.
type testFailure struct {
	file, test, describe, call, detail string
}

// printTestReport renders the elaborate summary block that follows
// runTests' per-assertion PASS/FAIL/SKIP log: a one-line-per-suite
// breakdown, aggregate counts, an enumerated failure list (when there are
// any), and a final banner. The very last line still reads exactly
// "%d passed, %d failed, %d incomplete" as its prefix — the substring
// existing callers and tests already key off of — so this is additive,
// not a breaking reformat.
func printTestReport(out io.Writer, style testReportStyle, files []fileReport, failures []testFailure, filesScanned int, elapsed time.Duration) {
	var totalPassed, totalFailed, totalSkipped, totalTests, totalDescribes int
	for _, fr := range files {
		totalTests += len(fr.results)
		for _, r := range fr.results {
			totalDescribes += len(r.Describes)
			p, f, sk := r.Counts()
			totalPassed += p
			totalFailed += f
			totalSkipped += sk
		}
	}
	totalAssertions := totalPassed + totalFailed + totalSkipped

	rule := strings.Repeat("=", 64)
	divider := strings.Repeat("-", 64)

	fmt.Fprintln(out)
	fmt.Fprintln(out, style.bold(rule))
	fmt.Fprintln(out, style.bold(" TEST REPORT"))
	fmt.Fprintln(out, style.bold(rule))
	for _, fr := range files {
		for _, r := range fr.results {
			p, f, sk := r.Counts()
			fmt.Fprintf(out, " %s %s :: %s — %d passed, %d failed, %d incomplete\n",
				style.statusIcon(f), filepath.Base(fr.file), r.Name, p, f, sk)
		}
	}
	fmt.Fprintln(out, divider)
	fmt.Fprintf(out, " Files       %d (%d with tests)\n", filesScanned, len(files))
	fmt.Fprintf(out, " Tests       %d\n", totalTests)
	fmt.Fprintf(out, " Describes   %d\n", totalDescribes)
	fmt.Fprintf(out, " Assertions  %d\n", totalAssertions)
	fmt.Fprintf(out, "   %s Passed      %d\n", style.green("✓"), totalPassed)
	fmt.Fprintf(out, "   %s Failed      %d\n", style.red("✗"), totalFailed)
	fmt.Fprintf(out, "   %s Incomplete  %d\n", style.yellow("○"), totalSkipped)

	if len(failures) > 0 {
		fmt.Fprintln(out, divider)
		fmt.Fprintln(out, " Failures:")
		for i, fl := range failures {
			fmt.Fprintf(out, "   %d) %s :: %s > %s > %s\n", i+1, filepath.Base(fl.file), fl.test, fl.describe, fl.call)
			fmt.Fprintf(out, "      %s\n", fl.detail)
		}
	}

	fmt.Fprintln(out, style.bold(rule))
	verdict := style.green("PASSED")
	if totalFailed > 0 {
		verdict = style.red("FAILED")
	}
	fmt.Fprintf(out, " %d passed, %d failed, %d incomplete — %s in %s\n", totalPassed, totalFailed, totalSkipped, verdict, elapsed.Round(time.Millisecond))
	fmt.Fprintln(out, style.bold(rule))
}
