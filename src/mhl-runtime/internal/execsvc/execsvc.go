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
	"errors"
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

	// Inputs are the pipeline's declared inputs. Unless Resume is set, they
	// are first checked against the pipeline's InputSchema (every declared
	// input present, no undeclared key — *runtime.InvalidInputsError if not),
	// then a string value is coerced against its declared `input name: Type`
	// (the `mhl run --input k=v` path) and a non-string value (the JSON path
	// an MCP/A2A adapter takes) is type-checked. Any failure returns here,
	// before a session or state dir is created and before any step runs.
	Inputs map[string]any

	// BaseDir is the directory the .mhl/ state tree lives under; "" means
	// the process working directory. A server gives each run its own dir so
	// concurrent runs never share checkpoint or memory state.
	BaseDir string

	// Session pins an explicit session id; "" resolves one (a fresh id, or —
	// with Resume — the most recent in-progress one via the .latest pointer).
	Session string
	Resume  bool

	// ForceResume, with Resume, downgrades a checkpoint definition-digest
	// mismatch from a hard error to a warning — the operator asserting the
	// edit since the checkpoint was written is safe to resume through. It does
	// not override an incompatible state-schema (that state is genuinely
	// unreadable).
	ForceResume bool

	// Principal is the verified caller identity, surfaced to a pipeline that
	// declares a `context:` block as read-only `context.principal`. "" for a
	// plain `mhl run` or when the serving layer has no token verifier.
	Principal string

	// StateStore, when non-nil, is where this run's per-step checkpoints and
	// result.json go instead of the on-disk .mhl/state tree — the serve
	// layer passes an extension-backed store (Phase 3) scoped to this run.
	// LoopStateStore is the same for a `loop pipeline`'s iteration
	// checkpoints; nil there falls back to disk.
	StateStore     runtime.StateStore
	LoopStateStore runtime.LoopStateStore

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
	// Paused is true when a step called pause(...): the run is suspended for a
	// human-in-the-loop hand-off, not finished — its checkpoint is kept and a
	// --resume / run/resume re-enters the pausing step. PauseReason carries the
	// pause argument.
	Paused      bool
	PauseReason any
	// Vars is the run's final variable state — the same map persisted as
	// result.json for a later run's `context:` to read. Populated on a normal
	// completion and on `break` (a clean early exit keeps its output); on a
	// `pause` it is the state as of the pause point. nil only when the run
	// failed with an error.
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

	// Admission check: enforce the pipeline's input contract (InputSchema) —
	// required inputs present, no undeclared keys — before creating a session,
	// state dir, or run. Strict always: an unrecognised input is an error, not
	// a silent no-op. Skipped on resume: the checkpoint owns the inputs then.
	if !req.Resume {
		if err := pipeline.ValidateInputs(req.Inputs); err != nil {
			return nil, err
		}
	}

	baseStore := runtime.NewStore(base)
	sessionID := runtime.ResolveSession(baseStore, pipeline.Name, req.Session, req.Resume)
	if sessionID != "" {
		fmt.Fprintf(out, "session: %s\n", sessionID)
	}

	// `context` is always visible to steps as this run's own metadata
	// (session_id / started_at / resumed / principal). A `context:` block is
	// only needed to also populate `context.vars` from a prior run — without
	// one, priorVars stays nil and contextSnapshot exposes `context.vars` as {}.
	var priorVars map[string]any
	if pipeline.Context != nil {
		pv, err := runtime.PriorVars(baseStore, pipeline.Name, pipeline.Context.Source)
		if err != nil {
			return nil, err
		}
		if pipeline.Context.Require && len(pv) == 0 {
			return nil, fmt.Errorf("context: source %q resolved to no stored state for pipeline %q", pipeline.Context.Source, pipeline.Name)
		}
		priorVars = pv
	}
	contextView := &interpreter.ContextView{
		SessionID: sessionID,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Resumed:   req.Resume,
		Principal: req.Principal,
		Vars:      priorVars,
	}

	// Same pure input type-check dry-run (execsvc.Inspect) runs — a mismatch
	// here must fail identically there.
	coercedInputs, inputErrs := coerceInputs(pipeline, req.Inputs)
	if len(inputErrs) > 0 {
		return nil, inputErrs[0]
	}

	memInit, err := interpreter.PipelineMemInit(prog, pipeline.Name)
	if err != nil {
		return nil, err
	}

	// projectVars decides what a finished run exposes to the caller. With an
	// `output: { ... }` mapping declared, only those keys leave the run
	// (evaluated against the final variable state); without one, the legacy
	// behaviour returns every non-internal `var`. It is only ever called for a
	// terminal run — a paused run reports publicVars(partial state) instead,
	// since its `output:` expressions may read vars that are not set yet.
	projectVars := func(finalVars map[string]any, instanceID string) (map[string]any, error) {
		if pipeline.Output == nil {
			return publicVars(finalVars), nil
		}
		mem := memContextFor(memInit, pipeline.Name, instanceID)
		return interpreter.EvalOutputs(runCtx, prog, pipeline.Output, file, out, store, jsonStore, mem, contextView, finalVars)
	}

	spawnSem := interpreter.NewSpawnSem(pipeline.Spawn.MaxConcurrency)

	stepTotal := len(pipeline.Steps)
	var stepSeq int64

	// runHook evaluates one of the pipeline's optional lifecycle hooks —
	// session_start, session_end, step_start, step_end, stop_failure — a
	// no-op when the pipeline declares none. instanceID picks which `mem`
	// namespace the hook body sees: "default" for a plain pipeline, the
	// loop's resolved instance for a `loop pipeline` (see startInstance /
	// loopInstance below).
	runHook := func(hookExpr *ast.Expr, hookName string, arg map[string]any, instanceID string, vars map[string]any) error {
		if hookExpr == nil {
			return nil
		}
		mem := memContextFor(memInit, pipeline.Name, instanceID)
		return interpreter.RunPipelineHook(runCtx, prog, pipeline.Name, hookName, hookExpr, arg, file, out, store, jsonStore, mem, contextView, vars)
	}

	// stopFailure fires the pipeline's stop_failure hook (if any) for a
	// genuine terminal failure — reason comes from *runtime.StepError.Kind
	// (the closed set "failed" | "timeout" | "cancelled" | "max_step_visits"),
	// or "runtime_error" for anything else that isn't step-shaped (a
	// checkpoint I/O error, an `output:`/context persistence failure). It
	// always returns err unchanged: a hook error here is only ever logged,
	// never allowed to mask the failure that triggered it.
	stopFailure := func(err error, instanceID string) error {
		arg := map[string]any{"pipeline": pipeline.Name, "kind": pipeline.Kind, "step": "", "reason": "runtime_error", "error": err.Error()}
		var stepErr *runtime.StepError
		if errors.As(err, &stepErr) {
			arg["step"] = stepErr.Step
			arg["reason"] = stepErr.Kind
			arg["error"] = stepErr.Unwrap().Error()
		}
		if hookErr := runHook(pipeline.StopFailure, "stop_failure", arg, instanceID, nil); hookErr != nil {
			fmt.Fprintf(out, "warning: %s.stop_failure: %v\n", pipeline.Name, hookErr)
		}
		return err
	}

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
		index := int(atomic.AddInt64(&stepSeq, 1))
		if req.OnStep != nil {
			req.OnStep(step, index, stepTotal)
		}
		// error is always present (StepContext declares it, types.go) — nil
		// here since the step hasn't run yet; step_end below fills it in.
		if err := runHook(pipeline.StepStart, "step_start", map[string]any{
			"pipeline": pipeline.Name, "step": step, "index": float64(index), "total": float64(stepTotal), "error": nil,
		}, ctx.InstanceID, ctx.Vars); err != nil {
			return err
		}
		mem := memContextFor(memInit, pipeline.Name, ctx.InstanceID)
		stepErr := interpreter.RunStep(stepCtx, prog, step, file, stepOut, store, jsonStore, ctx.Vars, mem, contextView, spawnSem)
		breakReason, isBreak := interpreter.IsBreak(stepErr)
		pauseReason, isPause := interpreter.IsPause(stepErr)
		isComplete := interpreter.IsComplete(stepErr)
		gotoTarget, isGoto := interpreter.IsGoto(stepErr)
		if pipeline.StepEnd != nil {
			var hookErrVal any
			if stepErr != nil && !isBreak && !isPause && !isComplete && !isGoto {
				hookErrVal = stepErr.Error()
			}
			if hookErr := runHook(pipeline.StepEnd, "step_end", map[string]any{
				"pipeline": pipeline.Name, "step": step, "index": float64(index), "total": float64(stepTotal), "error": hookErrVal,
			}, ctx.InstanceID, ctx.Vars); hookErr != nil {
				return hookErr
			}
		}
		switch {
		case isBreak:
			return &runtime.BreakSignal{Reason: breakReason}
		case isPause:
			return &runtime.PauseSignal{Reason: pauseReason}
		case isComplete:
			return &runtime.CompleteSignal{}
		case isGoto:
			return &runtime.GotoSignal{Target: gotoTarget}
		default:
			return stepErr
		}
	}

	init := pipelineVarsInit(prog, pipeline.Name, file, out, store, jsonStore, contextView)

	// Bind every checkpoint this run writes to the structure of the pipeline
	// it came from (its own declaration plus the shared decls it can
	// reference), so a later --resume refuses a checkpoint written for a
	// definition that has since changed shape (P1-9). --force downgrades that
	// to a warning.
	defDigest := runtime.DefinitionDigest(prog, pipeline.Name)

	// session_start fires exactly once, covering both the loop and non-loop
	// paths below, only after every pre-session admission check has already
	// passed — a bad input or memInit failure stays a pre-session error, not
	// something a hook author has to special-case as "session started then
	// instantly failed with no stop_failure". For a `loop pipeline`,
	// pipeline.InstanceID is resolved here too (mirroring, ahead of time,
	// what LoopRunner.Run would otherwise resolve on its own first call) so
	// the hook's `mem` and the loop's very first iteration share the same
	// instance — see LoopRunner.Run's doc comment for why it trusts a
	// pre-set InstanceID instead of re-resolving a fresh one.
	startInstance := "default"
	if pipeline.Loop {
		lr := runtime.NewLoopRunner(base).Session(sessionID)
		lr.Runner.WithDefinition(defDigest).WithForceResume(req.ForceResume)
		id, _, err := lr.ResolveInstanceID(pipeline.Name, req.Resume)
		if err != nil {
			return nil, err
		}
		pipeline.InstanceID = id
		startInstance = id
	}
	// Every SessionContext field is emitted on every firing (nil where this
	// one has nothing to say) so the single declared SessionContext type
	// (types.go) — shared by session_start and session_end alike — always
	// structurally matches; see the field's own doc comment there.
	if err := runHook(pipeline.SessionStart, "session_start", map[string]any{
		"pipeline": pipeline.Name, "kind": pipeline.Kind, "session_id": sessionID,
		"resumed": req.Resume, "inputs": coercedInputs,
		"vars": nil, "broke": nil, "break_reason": nil, "iterations": nil,
	}, startInstance, nil); err != nil {
		return nil, err
	}

	if !pipeline.Loop {
		runner := runtime.NewRunner(base).Session(sessionID).
			WithDefinition(defDigest).WithForceResume(req.ForceResume)
		runner.Out = out
		resultSink := runtime.StateStore(runner.Store)
		if req.StateStore != nil {
			runner.WithStateStore(req.StateStore)
			resultSink = req.StateStore
		}
		res, err := runner.Run(runCtx, pipeline, init, exec, req.Resume)
		if err != nil {
			return nil, stopFailure(err, "default")
		}
		// A paused run is suspended, not finished — don't overwrite the
		// session's "last completed run" result.json for a `context:` reader,
		// and don't run the `output:` projection: its expressions may read a
		// var the run has not reached yet. Surface the partial state; the
		// resume that completes the run evaluates the real projection. Same
		// reasoning excludes it from session_end/stop_failure: a paused run
		// isn't finished, in either direction.
		vars := publicVars(res.FinalVars)
		if !res.Paused {
			if err := persistContextResult(resultSink, pipeline, res.FinalVars); err != nil {
				return nil, stopFailure(err, "default")
			}
			if vars, err = projectVars(res.FinalVars, "default"); err != nil {
				return nil, stopFailure(err, "default")
			}
			if err := runHook(pipeline.SessionEnd, "session_end", map[string]any{
				"pipeline": pipeline.Name, "kind": pipeline.Kind, "session_id": sessionID,
				"resumed": nil, "inputs": nil,
				"vars": vars, "broke": res.Broke, "break_reason": res.BreakReason, "iterations": nil,
			}, "default", res.FinalVars); err != nil {
				return nil, err
			}
		}
		return &Result{
			PipelineName: pipeline.Name,
			SessionID:    sessionID,
			Executed:     res.Executed,
			Skipped:      res.Skipped,
			Resumed:      res.Resumed,
			Broke:        res.Broke,
			BreakReason:  res.BreakReason,
			Paused:       res.Paused,
			PauseReason:  res.PauseReason,
			Vars:         vars,
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
	loopRunner.Runner.WithDefinition(defDigest).WithForceResume(req.ForceResume)
	loopRunner.Runner.Out = out
	loopResultSink := runtime.StateStore(loopRunner.Runner.Store)
	if req.StateStore != nil {
		loopRunner.Runner.WithStateStore(req.StateStore)
		loopResultSink = req.StateStore
	}
	if req.LoopStateStore != nil {
		loopRunner.WithLoopStateStore(req.LoopStateStore)
	}
	res, err := loopRunner.Run(runCtx, pipeline, init, exec, evalStopWhen, req.Resume)
	if err != nil {
		return nil, stopFailure(err, pipeline.InstanceID)
	}
	paused := res.TerminalReason == "pause"
	loopInstance := res.InstanceID
	if loopInstance == "" {
		loopInstance = "default"
	}
	// As in the non-loop path: a paused loop reports its partial state, not the
	// `output:` projection, which is evaluated only once the run terminates.
	vars := publicVars(res.FinalVars)
	if !paused {
		if err := persistContextResult(loopResultSink, pipeline, res.FinalVars); err != nil {
			return nil, stopFailure(err, loopInstance)
		}
		if vars, err = projectVars(res.FinalVars, loopInstance); err != nil {
			return nil, stopFailure(err, loopInstance)
		}
		if err := runHook(pipeline.SessionEnd, "session_end", map[string]any{
			"pipeline": pipeline.Name, "kind": pipeline.Kind, "session_id": sessionID,
			"resumed": nil, "inputs": nil,
			"vars": vars, "broke": res.TerminalReason == "break", "break_reason": res.BreakReason,
			"iterations": float64(res.Iterations),
		}, loopInstance, res.FinalVars); err != nil {
			return nil, err
		}
	}
	return &Result{
		PipelineName:   pipeline.Name,
		SessionID:      sessionID,
		Resumed:        res.Resumed,
		Broke:          res.TerminalReason == "break",
		BreakReason:    res.BreakReason,
		Paused:         paused,
		PauseReason:    res.PauseReason,
		Vars:           vars,
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

// persistContextResult writes a run's final variable state as this session's
// result.json (and refreshes the .latest pointer), but only when the
// pipeline declared a `context:` block. A `break` counts as a completed run
// here (it carries its FinalVars); a nil finalVars — the run failed — writes
// nothing.
func persistContextResult(store runtime.StateStore, pipeline runtime.Pipeline, finalVars map[string]any) error {
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
