// Package execsvc runs an mhl pipeline or workflow programmatically and
// returns a structured result, with no CLI argument parsing and no
// human-readable summary printing. internal/cli's `run` subcommand is one
// caller; the MCP and A2A server adapters (internal/mcpserver,
// internal/a2aserver) are the others.
//
// It is a straight extraction of what `mhl run` already does between
// "arguments parsed" and "summary lines printed": read/parse the program,
// resolve imports, pick the pipeline, resolve a session directory, wire the
// `context:` view, coerce inputs against their declared types, and drive
// runtime.Runner (or runtime.LoopRunner for a `loop` pipeline). Progress
// output (`session: <id>`, `step: <name>`) still goes to Request.Out; the
// trailing summary is the caller's to format from Result.
//
// Session extensions are the caller's responsibility: register them once
// (interpreter.SetSessionExtensions) before calling Run.
package execsvc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// Request describes one execution.
type Request struct {
	// Context cancels the run at its next step (or loop iteration) boundary;
	// a step already in flight runs to completion. nil means no deadline
	// (context.Background). A server passes a per-request context here so
	// tasks/cancel and request timeouts actually stop the run.
	Context context.Context

	// Program, when non-nil, is a pre-parsed and import-resolved program —
	// the server preload path, where a directory of .mh files is parsed once
	// at startup and reused across requests. Otherwise Source names a .mh
	// file that Run reads, parses, and resolves imports for.
	Program *ast.Program
	Source  string

	// File is the path used for import/prompt resolution and in error
	// messages. Defaults to Source. With Program, set it to the file the
	// program was parsed from so relative `use`/`from "..."` paths resolve.
	File string

	// Workflow is the declared pipeline/workflow name to run; "" runs the
	// first one declared.
	Workflow string

	// Inputs are the pipeline's declared inputs. A string value is coerced
	// against its declared `input name: Type` (the `mhl run --input k=v`
	// path); a non-string value is assumed already typed (the JSON path an
	// MCP/A2A adapter takes) and only type-checked. Either way a bad value
	// fails here, before any step runs.
	Inputs map[string]any

	// BaseDir is the directory the .mhl/ state tree lives under; "" means
	// the process working directory. A server gives each run its own dir so
	// concurrent runs never share checkpoint or memory state.
	BaseDir string

	// Session pins an explicit session id; "" resolves one (a fresh id, or —
	// with Resume — the most recent in-progress one via the .latest pointer).
	Session string
	Resume  bool

	// Out receives progress output (`session:`/`step:` lines and anything a
	// step's log()/trace writes). nil discards it.
	Out io.Writer

	// OnStep, when non-nil, is called once just before each step runs, with
	// the step name, its 1-based position, and the pipeline's declared step
	// count (a `goto` or `loop` can push actual execution past that count).
	// It must not block. It may be called concurrently for the steps of a
	// `parallel` group, so an implementation must be safe for concurrent use.
	OnStep func(step string, index, total int)
}

// Result is the structured outcome of a run.
type Result struct {
	PipelineName string
	SessionID    string
	Executed     []string
	Skipped      []string
	Resumed      bool
	Broke        bool
	BreakReason  any
	// Vars is the run's final variable state — the same map persisted as
	// result.json for a later run's `context:` to read. nil when the run
	// broke or failed.
	Vars map[string]any

	// Loop is true when the pipeline carried the `loop` prefix; Iterations
	// and TerminalReason ("stop_when" | "max_iterations" | "break") are then
	// meaningful.
	Loop           bool
	Iterations     int
	TerminalReason string
}

// Run executes the pipeline described by req.
func Run(req Request) (*Result, error) {
	runCtx := req.Context
	if runCtx == nil {
		runCtx = context.Background()
	}
	out := req.Out
	if out == nil {
		out = io.Discard
	}
	base := req.BaseDir
	if base == "" {
		base = "."
	}

	prog := req.Program
	file := req.File
	if prog == nil {
		if req.Source == "" {
			return nil, fmt.Errorf("execsvc: neither Program nor Source given")
		}
		if file == "" {
			file = req.Source
		}
		src, err := os.ReadFile(req.Source)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", req.Source, err)
		}
		prog, err = parser.Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", req.Source, err)
		}
		if err := interpreter.ResolveImports(file, prog); err != nil {
			return nil, err
		}
	}

	store := memory.NewKVStore()
	jsonStore := memory.NewJSONStore()

	pipeline, err := runtime.FindPipeline(prog, req.Workflow)
	if err != nil {
		return nil, err
	}

	baseStore := runtime.NewStore(base)
	sessionID := runtime.ResolveSession(baseStore, pipeline.Name, req.Session, req.Resume)
	if sessionID != "" {
		fmt.Fprintf(out, "session: %s\n", sessionID)
	}

	var contextView *interpreter.ContextView
	if pipeline.Context != nil {
		priorVars, err := runtime.PriorVars(baseStore, pipeline.Name, pipeline.Context.Source)
		if err != nil {
			return nil, err
		}
		if pipeline.Context.Require && len(priorVars) == 0 {
			return nil, fmt.Errorf("context: source %q resolved to no stored state for pipeline %q", pipeline.Context.Source, pipeline.Name)
		}
		contextView = &interpreter.ContextView{
			SessionID: sessionID,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
			Resumed:   req.Resume,
			Vars:      priorVars,
		}
	}

	declaredInputs := map[string]types.Type{}
	for _, in := range pipeline.Inputs {
		declaredInputs[in.Name] = in.Type
	}
	coercedInputs := map[string]any{}
	for k, v := range req.Inputs {
		label := fmt.Sprintf("input %q", k)
		if raw, ok := v.(string); ok {
			cv, err := types.Coerce(label, declaredInputs[k], raw)
			if err != nil {
				return nil, err
			}
			coercedInputs[k] = cv
			continue
		}
		if err := types.Check(label, declaredInputs[k], v); err != nil {
			return nil, err
		}
		coercedInputs[k] = v
	}

	memInit, err := interpreter.PipelineMemInit(prog, pipeline.Name)
	if err != nil {
		return nil, err
	}

	spawnSem := interpreter.NewSpawnSem(pipeline.Spawn.MaxConcurrency)

	stepTotal := len(pipeline.Steps)
	var stepSeq int64

	exec := func(stepCtx context.Context, step string, ctx *runtime.RunContext) error {
		for k, v := range coercedInputs {
			ctx.Vars[k] = v
		}
		ctx.Vars["__last_step"] = step
		stepOut := out
		if ctx.Out != nil {
			stepOut = ctx.Out
		}
		fmt.Fprintf(stepOut, "step: %s\n", step)
		if req.OnStep != nil {
			req.OnStep(step, int(atomic.AddInt64(&stepSeq, 1)), stepTotal)
		}
		mem := memContextFor(memInit, pipeline.Name, ctx.InstanceID)
		err := interpreter.RunStep(stepCtx, prog, step, file, stepOut, store, jsonStore, ctx.Vars, mem, contextView, spawnSem)
		if reason, ok := interpreter.IsBreak(err); ok {
			return &runtime.BreakSignal{Reason: reason}
		}
		if target, ok := interpreter.IsGoto(err); ok {
			return &runtime.GotoSignal{Target: target}
		}
		return err
	}

	init := pipelineVarsInit(prog, pipeline.Name, file, out, store, jsonStore, contextView)

	if !pipeline.Loop {
		runner := runtime.NewRunner(base).Session(sessionID)
		runner.Out = out
		res, err := runner.Run(runCtx, pipeline, init, exec, req.Resume)
		if err != nil {
			return nil, err
		}
		if err := persistContextResult(runner.Store, pipeline, res.FinalVars); err != nil {
			return nil, err
		}
		return &Result{
			PipelineName: pipeline.Name,
			SessionID:    sessionID,
			Executed:     res.Executed,
			Skipped:      res.Skipped,
			Resumed:      res.Resumed,
			Broke:        res.Broke,
			BreakReason:  res.BreakReason,
			Vars:         publicVars(res.FinalVars),
		}, nil
	}

	evalStopWhen := func(instanceID string) (bool, error) {
		if pipeline.StopWhen == nil {
			return false, nil
		}
		mem := memContextFor(memInit, pipeline.Name, instanceID)
		return interpreter.EvalCondition(prog, pipeline.StopWhen, file, out, store, jsonStore, mem, contextView)
	}
	loopRunner := runtime.NewLoopRunner(base).Session(sessionID)
	loopRunner.Runner.Out = out
	res, err := loopRunner.Run(runCtx, pipeline, init, exec, evalStopWhen, req.Resume)
	if err != nil {
		return nil, err
	}
	if err := persistContextResult(loopRunner.Runner.Store, pipeline, res.FinalVars); err != nil {
		return nil, err
	}
	return &Result{
		PipelineName:   pipeline.Name,
		SessionID:      sessionID,
		Resumed:        res.Resumed,
		Broke:          res.TerminalReason == "break",
		BreakReason:    res.BreakReason,
		Vars:           publicVars(res.FinalVars),
		Loop:           true,
		Iterations:     res.Iterations,
		TerminalReason: res.TerminalReason,
	}, nil
}

// pipelineVarsInit returns a runtime.InitFunc that (re-)seeds ctx.Vars with
// pipelineName's top-level `var` declarations, evaluated fresh on every call
// (a pipeline var may read `memory`, so it must see the current state, not a
// setup-time snapshot).
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

// publicVars strips the runtime's own bookkeeping keys (e.g. __last_step) so
// what a caller sees — Result.Vars and the persisted result.json alike — is
// only the pipeline's declared inputs and vars. Returns nil for nil.
func publicVars(vars map[string]any) map[string]any {
	if vars == nil {
		return nil
	}
	clean := make(map[string]any, len(vars))
	for k, v := range vars {
		if strings.HasPrefix(k, "__") {
			continue
		}
		clean[k] = v
	}
	return clean
}

// persistContextResult writes a completed run's final variable state as this
// session's result.json (and refreshes the .latest pointer), but only when
// the pipeline declared a `context:` block. A nil finalVars (broke/failed)
// writes nothing.
func persistContextResult(store *runtime.Store, pipeline runtime.Pipeline, finalVars map[string]any) error {
	if pipeline.Context == nil || finalVars == nil {
		return nil
	}
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
// `mem` declarations for one run instance, or nil when the pipeline declares
// no `mem`. The backing file is isolated per instance under
// .mhl/state/mem/<pipeline>/<instanceID>.json.
func memContextFor(init map[string]*ast.Expr, pipelineName, instanceID string) *interpreter.MemContext {
	if len(init) == 0 {
		return nil
	}
	return &interpreter.MemContext{
		Path: filepath.Join(runtime.StateDirName, "mem", pipelineName, instanceID+".json"),
		Init: init,
	}
}
