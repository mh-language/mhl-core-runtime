// Package cli implements the mhl command-line surface: argument parsing and
// dispatch for `mhl run`, `mhl test`, `mhl skills list`, and `mhl lint`. It
// parses a .mh file with internal/lang/parser, then hands the resulting
// AST to internal/engine/interpreter (for `run`/`test`) or internal/lang/lint
// (for `lint`) to do the actual work — this package owns none of the
// language evaluation itself.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/engine/interpreter"
	"github.com/yanjustino/mhl-runtime/internal/engine/runtime"
	"github.com/yanjustino/mhl-runtime/internal/features/memory"
	"github.com/yanjustino/mhl-runtime/internal/features/skills"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
	"github.com/yanjustino/mhl-runtime/internal/lang/lint"
	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
	"github.com/yanjustino/mhl-runtime/internal/lang/types"
	"github.com/yanjustino/mhl-runtime/internal/lsp"
)

// Run dispatches a mhl subcommand. It writes user-facing output to out and
// returns a non-nil error on failure.
func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl <command> [args]\n  commands: run, test, skills, lint, lsp")
	}
	switch args[0] {
	case "skills":
		return runSkills(args[1:], out)
	case "run":
		return runPipeline(args[1:], out)
	case "test":
		return runTests(args[1:], out)
	case "lint":
		return runLint(args[1:], out)
	case "lsp":
		return runLSP(out)
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
//	mhl run <pipeline.mh> [--input key=value ...] [--resume]
//
// It executes a pipeline from the start, or resumes it from the last saved
// checkpoint when --resume is given (IF-1).
func runPipeline(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl run <pipeline.mh> [--input key=value ...] [--resume]")
	}
	file := args[0]
	resume := false
	inputs := map[string]string{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--resume":
			resume = true
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

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", file, err)
	}
	if err := interpreter.ResolveImports(file, prog); err != nil {
		return err
	}

	store := memory.NewKVStore()
	jsonStore := memory.NewJSONStore()

	pipeline, err := runtime.FindPipeline(prog, "")
	if err != nil {
		return err
	}
	// Coerce each --input value toward its declared `input name: Type`
	// (types.Any, i.e. a raw string, for anything not declared) once, before
	// any step runs — a malformed value (e.g. --input count=abc against
	// `input count: number`) fails immediately here instead of surfacing deep
	// inside evalAdd/evalMul on whatever step first uses it.
	declaredInputs := map[string]types.Type{}
	for _, in := range pipeline.Inputs {
		declaredInputs[in.Name] = in.Type
	}
	coercedInputs := map[string]any{}
	for k, raw := range inputs {
		v, err := types.Coerce(fmt.Sprintf("input %q", k), declaredInputs[k], raw)
		if err != nil {
			return err
		}
		coercedInputs[k] = v
	}
	// memInit is computed once, outside exec/evalStopWhen: a pipeline's
	// `mem` declarations (which names exist, and their get-or-init
	// initializer expressions) never change across steps or iterations —
	// only the instance id each closure combines it with (ctx.InstanceID,
	// evalStopWhen's own instanceID parameter) does. nil for a pipeline with
	// no `mem` at all, so memContextFor below returns a nil *MemContext too.
	memInit, err := interpreter.PipelineMemInit(prog, pipeline.Name)
	if err != nil {
		return err
	}

	exec := func(step string, ctx *runtime.RunContext) error {
		for k, v := range coercedInputs {
			ctx.Vars[k] = v
		}
		ctx.Vars["__last_step"] = step
		fmt.Fprintf(out, "step: %s\n", step)
		mem := memContextFor(memInit, pipeline.Name, ctx.InstanceID)
		err := interpreter.RunStep(prog, step, file, out, store, jsonStore, ctx.Vars, mem)
		// interpreter and runtime each define their own break/goto signal
		// type and stay independent of one another (see runtime/signal.go);
		// this closure is the one place both are in scope to translate
		// between them, the same seam it already is for everything else
		// crossing that boundary.
		if reason, ok := interpreter.IsBreak(err); ok {
			return &runtime.BreakSignal{Reason: reason}
		}
		if target, ok := interpreter.IsGoto(err); ok {
			return &runtime.GotoSignal{Target: target}
		}
		return err
	}

	init := pipelineVarsInit(prog, pipeline.Name, file, out, store, jsonStore)

	// A `loop pipeline` repeats itself until its stop_when is satisfied, its
	// max_iterations ceiling is hit, or a step explicitly `break`s — see
	// runtime.LoopRunner. A plain pipeline (the common case today) falls
	// straight through to running once, unchanged.
	if !pipeline.Loop {
		runner := runtime.NewRunner(".")
		res, err := runner.Run(pipeline, init, exec, resume)
		if err != nil {
			return err
		}
		if res.Resumed {
			fmt.Fprintf(out, "resumed pipeline %q; skipped %d completed step(s)\n", pipeline.Name, len(res.Skipped))
		}
		fmt.Fprintf(out, "executed %d step(s)\n", len(res.Executed))
		if res.Broke {
			printBreakReason(out, pipeline.Name, res.BreakReason)
		}
		return nil
	}

	evalStopWhen := func(instanceID string) (bool, error) {
		if pipeline.StopWhen == nil {
			return false, nil
		}
		mem := memContextFor(memInit, pipeline.Name, instanceID)
		return interpreter.EvalCondition(prog, pipeline.StopWhen, file, out, store, jsonStore, mem)
	}
	loopRunner := runtime.NewLoopRunner(".")
	res, err := loopRunner.Run(pipeline, init, exec, evalStopWhen, resume)
	if err != nil {
		return err
	}
	if res.Resumed {
		fmt.Fprintf(out, "resumed loop %q at iteration %d\n", pipeline.Name, res.Iterations)
	}
	fmt.Fprintf(out, "loop %q ran %d iteration(s), stopped: %s\n", pipeline.Name, res.Iterations, res.TerminalReason)
	if res.TerminalReason == "break" {
		printBreakReason(out, pipeline.Name, res.BreakReason)
	}
	return nil
}

// pipelineVarsInit returns a runtime.InitFunc that (re-)seeds ctx.Vars with
// pipelineName's top-level `var` declarations (interpreter.EvalPipelineVars)
// — evaluated fresh on *every* call, not once here at setup time, since a
// pipeline var's expression may read `memory` (e.g. a pending-features
// list), and each call corresponds to one fresh Runner.Run() — one loop
// iteration — so it must see that iteration's current memory state, not a
// stale snapshot from the first one. ctx.Vars already exists (Run
// allocates it before calling init) and is also where --input/__last_step
// live (see exec above); merging pipeline vars into that same map, rather
// than keeping a separate one, is what lets a single ctx.Vars argument
// carry all of a step's non-step-local state through to
// interpreter.RunStep — a pipeline var and an --input flag sharing a name
// would collide there, the same class of foot-gun as any other
// reserved-name collision in the language.
func pipelineVarsInit(prog *ast.Program, pipelineName, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore) runtime.InitFunc {
	return func(ctx *runtime.RunContext) error {
		env, err := interpreter.EvalPipelineVars(prog, pipelineName, file, out, store, jsonStore)
		if err != nil {
			return err
		}
		for k, v := range env {
			ctx.Vars[k] = v
		}
		return nil
	}
}

// memContextFor builds the interpreter.MemContext backing pipelineName's
// `mem` declarations for one specific run instance, or nil when init is nil
// (the pipeline declares no `mem` at all — the common case, and the only
// one RunStep/EvalCondition need to treat specially, since a nil
// *MemContext just means their own mem fallback tier never fires). The
// backing file is isolated per instance (.mhl/state/mem/<pipeline>/
// <instanceID>.json) — see runtime.RunContext.InstanceID and
// LoopRunner.Run's id resolution for what instanceID actually is: "default"
// for a plain pipeline, a fresh or resumed loop-run id for a `loop
// pipeline`.
func memContextFor(init map[string]*ast.Expr, pipelineName, instanceID string) *interpreter.MemContext {
	if len(init) == 0 {
		return nil
	}
	return &interpreter.MemContext{
		Path: filepath.Join(runtime.StateDirName, "mem", pipelineName, instanceID+".json"),
		Init: init,
	}
}

func printBreakReason(out io.Writer, name string, reason any) {
	if reason != nil {
		fmt.Fprintf(out, "%q stopped by break: %v\n", name, reason)
	} else {
		fmt.Fprintf(out, "%q stopped by break\n", name)
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

func runSkills(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: mhl skills list [dir]")
	}
	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}
	return skillsList(dir, out)
}

// skillsList scans dir for .mh files, parses each, and prints every declared
// skill with its declared tools and mcp_servers.
func skillsList(dir string, out io.Writer) error {
	files, err := findMHLFiles(dir)
	if err != nil {
		return err
	}

	var defs []*skills.SkillDefinition
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}
		prog, err := parser.Parse(string(src))
		if err != nil {
			return fmt.Errorf("parsing %s: %w", f, err)
		}
		defs = append(defs, skills.ListSkills(prog)...)
	}

	if len(defs) == 0 {
		fmt.Fprintln(out, "No skills declared in the current project.")
		return nil
	}

	for _, d := range defs {
		fmt.Fprintf(out, "Skill: %s\n", d.Name)
		if d.Description != "" {
			fmt.Fprintf(out, "  description: %s\n", d.Description)
		}
		fmt.Fprintf(out, "  tools: [%s]\n", strings.Join(d.Tools, ", "))
		fmt.Fprintf(out, "  mcp_servers: [%s]\n", strings.Join(d.MCPServers, ", "))
	}
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
