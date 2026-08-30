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

	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	_ "github.com/mh-language/mhl-core-runtime/internal/extbuiltin" // registers the built-in MCP/A2A extensions
	"github.com/mh-language/mhl-core-runtime/internal/extension/external"
	"github.com/mh-language/mhl-core-runtime/internal/features/a2a"
	"github.com/mh-language/mhl-core-runtime/internal/features/mcp"
	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
	"github.com/mh-language/mhl-core-runtime/internal/lsp"
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
}

// Run dispatches a mhl subcommand. It writes user-facing output to out and
// returns a non-nil error on failure.
func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl <command> [args]\n  commands: init, run, test, lint, lsp, extension, version")
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
	inputs := map[string]string{}
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

	// Every execution gets its own session directory under .mhl/state, so two
	// concurrent `mhl run`s of one pipeline never clobber each other's
	// checkpoint. A bare --resume follows the .latest pointer; --session pins
	// an explicit one. An empty id means "resume a pre-session checkpoint" —
	// the runner stays unscoped for back-compat.
	baseStore := runtime.NewStore(".")
	sessionID := runtime.ResolveSession(baseStore, pipeline.Name, sessionFlag, resume)
	if sessionID != "" {
		fmt.Fprintf(out, "session: %s\n", sessionID)
	}

	// contextView backs a pipeline's `context:` element — nil when it
	// declares none. It carries this run's session metadata plus the variable
	// state left by a prior run (per context.source), read-only, into every
	// step, the stop_when condition, and pipeline-level `var` initializers.
	var contextView *interpreter.ContextView
	if pipeline.Context != nil {
		priorVars, err := runtime.PriorVars(baseStore, pipeline.Name, pipeline.Context.Source)
		if err != nil {
			return err
		}
		if pipeline.Context.Require && len(priorVars) == 0 {
			return fmt.Errorf("context: source %q resolved to no stored state for pipeline %q", pipeline.Context.Source, pipeline.Name)
		}
		contextView = &interpreter.ContextView{
			SessionID: sessionID,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
			Resumed:   resume,
			Vars:      priorVars,
		}
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

	// One semaphore for the whole run: `spawn: { max_concurrency: N }` caps
	// concurrent spawned agent calls across every step, not per step.
	spawnSem := interpreter.NewSpawnSem(pipeline.Spawn.MaxConcurrency)

	exec := func(step string, ctx *runtime.RunContext) error {
		for k, v := range coercedInputs {
			ctx.Vars[k] = v
		}
		ctx.Vars["__last_step"] = step
		// A `parallel` branch step runs with ctx.Out set to a per-branch
		// buffer so concurrent branches never interleave on stdout; every
		// other step leaves it nil and writes straight to `out`.
		stepOut := out
		if ctx.Out != nil {
			stepOut = ctx.Out
		}
		fmt.Fprintf(stepOut, "step: %s\n", step)
		mem := memContextFor(memInit, pipeline.Name, ctx.InstanceID)
		err := interpreter.RunStep(prog, step, file, stepOut, store, jsonStore, ctx.Vars, mem, contextView, spawnSem)
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

	init := pipelineVarsInit(prog, pipeline.Name, file, out, store, jsonStore, contextView)

	// A `loop pipeline` repeats itself until its stop_when is satisfied, its
	// max_iterations ceiling is hit, or a step explicitly `break`s — see
	// runtime.LoopRunner. A plain pipeline (the common case today) falls
	// straight through to running once, unchanged.
	if !pipeline.Loop {
		runner := runtime.NewRunner(".").Session(sessionID)
		runner.Out = out
		res, err := runner.Run(pipeline, init, exec, resume)
		if err != nil {
			return err
		}
		if err := persistContextResult(runner.Store, pipeline, res.FinalVars); err != nil {
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
		return interpreter.EvalCondition(prog, pipeline.StopWhen, file, out, store, jsonStore, mem, contextView)
	}
	loopRunner := runtime.NewLoopRunner(".").Session(sessionID)
	loopRunner.Runner.Out = out
	res, err := loopRunner.Run(pipeline, init, exec, evalStopWhen, resume)
	if err != nil {
		return err
	}
	if err := persistContextResult(loopRunner.Runner.Store, pipeline, res.FinalVars); err != nil {
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
func pipelineVarsInit(prog *ast.Program, pipelineName, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore, contextView *interpreter.ContextView) runtime.InitFunc {
	return func(ctx *runtime.RunContext) error {
		env, err := interpreter.EvalPipelineVars(prog, pipelineName, file, out, store, jsonStore, contextView)
		if err != nil {
			return err
		}
		for k, v := range env {
			ctx.Vars[k] = v
		}
		return nil
	}
}

// persistContextResult writes a completed run's final variable state as this
// session's result.json (and refreshes the .latest pointer), but only when
// the pipeline declared a `context:` block — the one consumer of it. A nil
// finalVars (the run broke or failed) writes nothing.
func persistContextResult(store *runtime.Store, pipeline runtime.Pipeline, finalVars map[string]any) error {
	if pipeline.Context == nil || finalVars == nil {
		return nil
	}
	// Drop the runtime's own bookkeeping entries (e.g. __last_step) so
	// context.vars only ever exposes the pipeline's declared inputs and vars.
	clean := make(map[string]any, len(finalVars))
	for k, v := range finalVars {
		if strings.HasPrefix(k, "__") {
			continue
		}
		clean[k] = v
	}
	return store.WriteResult(pipeline.Name, clean)
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
