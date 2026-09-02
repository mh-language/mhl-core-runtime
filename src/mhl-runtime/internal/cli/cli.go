// Package cli implements the mhl command-line surface: argument parsing and
// dispatch for `mhl run`, `mhl test`, and `mhl lint`. It parses a .mh file
// with internal/lang/parser, then hands the resulting AST to
// internal/engine/interpreter (for `run`/`test`) or internal/lang/lint (for
// `lint`) to do the actual work — this package owns none of the language
// evaluation itself.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/a2aserver"
	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	_ "github.com/mh-language/mhl-core-runtime/internal/extbuiltin" // registers the built-in MCP/A2A extensions
	"github.com/mh-language/mhl-core-runtime/internal/extension/external"
	"github.com/mh-language/mhl-core-runtime/internal/features/a2a"
	"github.com/mh-language/mhl-core-runtime/internal/features/mcp"
	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lsp"
	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

// Version is the mhl CLI's version string, reported by `mhl version` (and
// the `--version`/`-v` aliases). It defaults to "dev" for a plain `go
// build`; the Makefile's `build`/`release` targets inject the real value at
// link time via `-ldflags -X`, derived from `git describe` — so a binary
// built outside that Makefile (or without a tag reachable from HEAD) always
// says "dev" rather than a stale or fabricated version number.
var Version = "dev"

// The MCP client (internal/features/mcp) reports a clientInfo.version on its
// handshake; it sits below this package in the dependency order and can't read
// Version itself, so hand it over. Some MCP servers reject an empty version.
//
// The built-in runtime extensions (MCP, A2A) register themselves into
// internal/extension via the blank import of internal/extbuiltin above; here
// we only hand them the CLI version string (see docs/plano-extensoes-mcp-a2a.md).
func init() {
	mcp.ClientVersion = Version
	mcp.ExtensionVersion = Version
	a2a.ExtensionVersion = Version
	external.SetHostVersion(Version)
	mcpserver.ServerVersion = Version
	a2aserver.ServerVersion = Version
}

// Run dispatches a mhl subcommand. It writes user-facing output to out and
// returns a non-nil error on failure.
func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl <command> [args]\n  commands: init, run, test, lint, lsp, serve, extension, version")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], out)
	case "run":
		return runPipeline(args[1:], out)
	case "test":
		return runTests(args[1:], out)
	case "lint":
		return runLint(args[1:], out)
	case "lsp":
		return runLSP(out)
	case "serve":
		return runServe(args[1:], out)
	case "extension":
		return runExtension(args[1:], out)
	case "version", "--version", "-v":
		fmt.Fprintln(out, "mhl", Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// runLSP implements:
//
//	mhl lsp
//
// It starts a Language Server Protocol server on stdin/stdout for editor
// integrations (see vscode-mhl). out must be exactly the client's stdin —
// unlike every other subcommand, nothing but LSP-framed JSON-RPC may ever
// be written to it, so this stays the one command that treats out as a raw
// protocol stream rather than as user-facing text.
func runLSP(out io.Writer) error {
	return lsp.Serve(os.Stdin, out)
}

// runPipeline implements:
//
//	mhl run <pipeline.mh> [--input key=value ...] [--resume] [--session <id>]
//
// It executes a pipeline from the start, or resumes it from the last saved
// checkpoint when --resume is given (IF-1).
func runPipeline(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl run <pipeline.mh> [--input key=value ...] [--resume]")
	}
	defer loadSessionExtensions(out)()
	file := args[0]
	resume := false
	sessionFlag := ""
	inputs := map[string]any{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--resume":
			resume = true
		case "--session":
			if i+1 >= len(args) {
				return fmt.Errorf("--session requires an id argument")
			}
			i++
			sessionFlag = args[i]
		case "--input":
			if i+1 >= len(args) {
				return fmt.Errorf("--input requires a key=value argument")
			}
			i++
			k, v, ok := strings.Cut(args[i], "=")
			if !ok {
				return fmt.Errorf("--input expects key=value, got %q", args[i])
			}
			inputs[k] = v
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	res, err := execsvc.Run(execsvc.Request{
		Source:  file,
		Inputs:  inputs,
		BaseDir: ".",
		Session: sessionFlag,
		Resume:  resume,
		Out:     out,
	})
	if err != nil {
		return err
	}

	if !res.Loop {
		if res.Resumed {
			fmt.Fprintf(out, "resumed pipeline %q; skipped %d completed step(s)\n", res.PipelineName, len(res.Skipped))
		}
		fmt.Fprintf(out, "executed %d step(s)\n", len(res.Executed))
		if res.Broke {
			printBreakReason(out, res.PipelineName, res.BreakReason)
		}
		if res.Paused {
			printPauseReason(out, res.PipelineName, res.PauseReason)
		}
		return nil
	}

	if res.Resumed {
		fmt.Fprintf(out, "resumed loop %q at iteration %d\n", res.PipelineName, res.Iterations)
	}
	fmt.Fprintf(out, "loop %q ran %d iteration(s), stopped: %s\n", res.PipelineName, res.Iterations, res.TerminalReason)
	if res.TerminalReason == "break" {
		printBreakReason(out, res.PipelineName, res.BreakReason)
	}
	if res.TerminalReason == "pause" {
		printPauseReason(out, res.PipelineName, res.PauseReason)
	}
	return nil
}

func printBreakReason(out io.Writer, name string, reason any) {
	if reason != nil {
		fmt.Fprintf(out, "%q stopped by break: %v\n", name, reason)
	} else {
		fmt.Fprintf(out, "%q stopped by break\n", name)
	}
}

func printPauseReason(out io.Writer, name string, reason any) {
	if reason != nil {
		fmt.Fprintf(out, "%q paused (resume with --resume): %v\n", name, reason)
	} else {
		fmt.Fprintf(out, "%q paused (resume with --resume)\n", name)
	}
}

// runTests implements:
//
//	mhl test <file.mh>
//	mhl test <dir>
//
// Given a file, it runs every `test { ... }` block declared in it. Given a
// directory, it recursively finds every .mh file under it (same traversal
// as `mhl lint`) and runs the test blocks declared in each of them, in
// sorted order, aggregating one summary across all of them; files with no
// test blocks are skipped rather than treated as an error, since not every
// .mh file under a directory is expected to declare tests.
//
// Each describe block's assertion results are printed PASS/FAIL/SKIP per
// assertion (with a colored ✓/✗/○ glance-icon when out is a real terminal —
// see isTerminalWriter), followed by a per-describe subtotal, and finally
// an elaborate report across every suite (printTestReport, test_report.go):
// a one-line-per-suite breakdown, aggregate counts, an enumerated list of
// every failure's file/test/describe address, and a pass/fail banner. It
// returns a non-nil error (and so a non-zero exit code from cmd/mhl) when
// any assertion failed, or when no test blocks were found at all; an
// `incomplete(...)` assertion does not count as a failure.
func runTests(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl test <file.mh|dir>")
	}
	defer loadSessionExtensions(out)()
	target := args[0]
	start := time.Now()

	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("reading %s: %w", target, err)
	}

	var files []string
	if info.IsDir() {
		files, err = findMHLFiles(target)
		if err != nil {
			return err
		}
	} else {
		files = []string{target}
	}

	style := testReportStyle{color: isTerminalWriter(out)}
	var fileReports []fileReport
	var failures []testFailure
	anyFailed := false
	for _, file := range files {
		results, err := runTestFile(file, out)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			continue
		}
		fileReports = append(fileReports, fileReport{file: file, results: results})

		for _, r := range results {
			fmt.Fprintf(out, "test %s\n", r.Name)
			for _, d := range r.Describes {
				fmt.Fprintf(out, "  describe %s\n", d.Name)
				dPassed, dFailed, dSkipped := 0, 0, 0
				for _, a := range d.Assertions {
					switch {
					case a.Skipped:
						dSkipped++
						fmt.Fprintf(out, "    %s %s\n", style.yellow("○ SKIP"), a.Detail)
					case a.Passed:
						dPassed++
						fmt.Fprintf(out, "    %s %s\n", style.green("✓ PASS"), a.Call)
					default:
						dFailed++
						fmt.Fprintf(out, "    %s %s — %s\n", style.red("✗ FAIL"), a.Call, a.Detail)
						failures = append(failures, testFailure{file: file, test: r.Name, describe: d.Name, call: a.Call, detail: a.Detail})
					}
				}
				fmt.Fprintf(out, "  %s %d passed, %d failed, %d incomplete\n", style.statusIcon(dFailed), dPassed, dFailed, dSkipped)
			}
			if r.Failed() {
				anyFailed = true
			}
		}
	}

	if len(fileReports) == 0 {
		return fmt.Errorf("no test blocks declared in %s", target)
	}

	printTestReport(out, style, fileReports, failures, len(files), time.Since(start))
	if anyFailed {
		return fmt.Errorf("%d assertion(s) failed", len(failures))
	}
	return nil
}

// runTestFile parses file and runs its declared test blocks, returning the
// raw per-test results for the caller to print and aggregate.
func runTestFile(file string, out io.Writer) ([]*interpreter.TestResult, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", file, err)
	}
	if err := interpreter.ResolveImports(file, prog); err != nil {
		return nil, err
	}

	store := memory.NewKVStore()
	jsonStore := memory.NewJSONStore()
	return interpreter.RunTests(prog, file, out, store, jsonStore)
}

// runLint implements:
//
//	mhl lint [dir]
//
// It recursively scans dir (defaulting to ".") for .mh files and statically
// reports every problem that would otherwise only surface when `mhl run`
// happens to reach the offending code (broken import/use, undeclared agent,
// misconfigured agent, syntax error), each with a file and line number.
// initScaffold is what `mhl init` writes as a new project's main.mh. It
// deliberately uses `command: "echo"` instead of a real LLM CLI so `mhl run`
// works immediately after `mhl init`, with no external binary or credential
// required — the comment tells the reader how to swap in a real agent once
// they're ready.
const initScaffold = `// Generated by ` + "`mhl init`" + `. Try it right away:
//   mhl run main.mh --input name=World
//
// Swap the "echo" command below for a real CLI agent (e.g. "claude",
// "codex") when you're ready — see the language reference
// (docs/site/reference.html, or https://mh-language.github.io/mhl-core-runtime/reference.html)
// for engine/args/retry/cache/before/after.
agent Greeter {
    command: "echo"
    args: ["${prompt}"]
}

pipeline Main {
    input name: string

    step Greet {
        var response = Greeter.run(prompt: "Hello, ${name}!")
        log(response)
    }
}
`

// runInit scaffolds a new project: a single, immediately-runnable main.mh
// in dir (default "."). It never overwrites an existing file — fail closed,
// the same stance the rest of this package takes toward anything that could
// destroy a user's existing work.
func runInit(args []string, out io.Writer) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, "main.mh")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; mhl init never overwrites an existing file", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(initScaffold), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Fprintf(out, "created %s\n", path)
	fmt.Fprintln(out, "next steps:")
	fmt.Fprintf(out, "  mhl run %s --input name=World\n", path)
	fmt.Fprintf(out, "  mhl lint %s\n", dir)
	return nil
}

func runLint(args []string, out io.Writer) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	findings, err := lint.Dir(dir)
	if err != nil {
		return err
	}
	for _, f := range findings {
		fmt.Fprintln(out, f.String())
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "%d problem(s) found\n", len(findings))
		return fmt.Errorf("%d problem(s) found", len(findings))
	}
	fmt.Fprintln(out, "No problems found.")
	return nil
}

// findMHLFiles returns the sorted list of .mh files under dir (recursively).
func findMHLFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".mh") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
