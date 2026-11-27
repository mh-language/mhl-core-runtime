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
	"maps"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/engine/value"
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
	// NoCache makes every agent call of the run go out again instead of
	// returning a cached response (`mhl run --no-cache`); each fresh
	// response replaces the cached one (interpreter.WithoutAgentCache).
	NoCache bool
	// Decision answers the typed pause — `pause(reason, decision: T)` — a
	// suspended run waits at (Result.Awaiting): a JSON value of type T (an
	// object, or a string/number/bool; a string is also accepted for an
	// object or array as its JSON text). Requires Resume. It is checked
	// against T before any step runs and handed to that one gate, which
	// returns it; a run not waiting for a decision refuses one
	// (*DecisionError). nil resumes as before: a gate pauses again.
	Decision any
	// From redoes a phase (`mhl run --resume --from <stage>`): the resume
	// re-enters that named stage at its first member instead of continuing
	// at the step the run stopped at, with the variables as of the stop (no
	// rollback) and any gate the run waited at dropped. Requires Resume and
	// a checkpoint; refused with Decision (*ResumeFromError).
	From string

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

	// OnStart, when non-nil, is called once, after inputs are validated and
	// the session is resolved and before any step runs, with the pipeline
	// name, the session id and its declared steps in execution order.
	OnStart func(pipeline, session string, steps []string)
	// OnStages, when non-nil, is called once just before OnStart with the
	// pipeline's named stages (nil without any) — the step → stage map a
	// caller tags step events with.
	OnStages func(stages []runtime.NamedStage)

	// OnStepEnd, when non-nil, is called once each time a step finishes,
	// right after its body returns (before the step_end hook). status is
	// one of "ok", "failed", "goto" (target names the destination),
	// "pause", "break" or "complete"; err is the step's error for
	// "failed", nil otherwise. Same concurrency contract as OnStep.
	OnStepEnd func(step string, index int, status, target string, err error, elapsed time.Duration)
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
	// Awaiting is set when the pause is a typed one: the gate the run waits
	// at and the decision a resume must supply (Request.Decision).
	Awaiting *Awaiting
	// AwaitingInputs is set when the run paused on entering a named stage
	// whose required inputs are unbound: the inputs a resume must supply
	// (Request.Inputs, checked by Pipeline.ValidateStageInputs).
	AwaitingInputs *AwaitingInputs
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

// Awaiting describes the gate — `pause(reason, decision: T)` — a paused run
// waits at.
type Awaiting struct {
	Step string `json:"step"`
	// Index tells apart several gates of one step (0-based, evaluation order).
	Index int `json:"index"`
	// Type is T as written; Schema is it as JSON Schema (enum variants filled
	// in), what a client renders a decision form from.
	Type   string         `json:"type"`
	Schema map[string]any `json:"schema,omitempty"`
	// Name is what the program binds the decision to (`var approved =
	// pause(...)`), Description its `///` doc — a form's label and help.
	// The doc is also Schema's "description".
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// AwaitingInputs describes the stage inputs a paused run waits for
// (runtime.InputGate): the stage, the unit it stopped before, the missing
// inputs in declaration order, and them as one JSON Schema object — what a
// client renders a form from.
type AwaitingInputs struct {
	Stage  string         `json:"stage"`
	Step   string         `json:"step"`
	Inputs []string       `json:"inputs"`
	Schema map[string]any `json:"schema,omitempty"`
}

// awaitingInputsOf describes gate for the caller; nil for a nil gate.
func awaitingInputsOf(pipeline runtime.Pipeline, gate *runtime.InputGate) *AwaitingInputs {
	if gate == nil {
		return nil
	}
	return &AwaitingInputs{
		Stage:  gate.Stage,
		Step:   gate.Step,
		Inputs: append([]string(nil), gate.Inputs...),
		Schema: pipeline.StageInputSchema(gate.Stage, gate.Inputs),
	}
}

// AwaitingInputsFor describes gate for a run of w, as Result.AwaitingInputs
// does — for a server that knows a run's gate only from its checkpoint.
func AwaitingInputsFor(w Workflow, gate *runtime.InputGate) *AwaitingInputs {
	return awaitingInputsOf(w.Pipeline, gate)
}

// ResumeFromError reports a Request.From that cannot be applied: no such
// stage, no stopped run, or not a plain resumable pipeline. Nothing ran.
type ResumeFromError struct{ Msg string }

func (e *ResumeFromError) Error() string { return "from: " + e.Msg }

// DecisionError reports a Request.Decision that cannot be applied: the run
// is not waiting at a gate, or the value is not of the gate's type. Nothing
// ran; a paused run stays paused.
type DecisionError struct{ Msg string }

func (e *DecisionError) Error() string { return "decision: " + e.Msg }

// awaitingOf describes gate for the caller, its schema rendered from the
// program's declarations (nil when the type no longer resolves).
func awaitingOf(prog *ast.Program, pipeline runtime.Pipeline, gate *runtime.Gate) *Awaiting {
	if gate == nil {
		return nil
	}
	a := &Awaiting{Step: gate.Step, Index: gate.Index, Type: gate.Type, Name: gate.Name, Description: gate.Doc}
	if t, ok := interpreter.ResolveDecisionType(prog, gate.Type); ok {
		a.Schema = pipeline.TypeSchema(t, gate.Type)
		if a.Schema != nil && a.Description != "" {
			a.Schema["description"] = a.Description
		}
	}
	return a
}

// AwaitingFor describes gate for a run of w, as Result.Awaiting does — for a
// server that knows a run's gate only from its checkpoint.
func AwaitingFor(w Workflow, gate *runtime.Gate) *Awaiting {
	return awaitingOf(w.Program, w.Pipeline, gate)
}

// CheckDecision reports, as a *DecisionError, whether decision cannot answer
// a gate of w waiting for type typeName — what a server checks before it
// relaunches a paused run, so a bad decision is refused while the run stays
// paused. Run checks it again against the checkpoint.
func CheckDecision(w Workflow, typeName string, decision any) error {
	t, ok := interpreter.ResolveDecisionType(w.Program, typeName)
	if !ok {
		return &DecisionError{Msg: fmt.Sprintf("the gate waits for an unknown type %q", typeName)}
	}
	if _, err := coerceOne(typeName, t, decision, w.Pipeline.Enums); err != nil {
		return &DecisionError{Msg: err.Error()}
	}
	return nil
}

// gateAnswer checks decision against the gate cp is suspended at, and
// returns the answer for that gate.
func gateAnswer(prog *ast.Program, pipeline runtime.Pipeline, cp *runtime.Checkpoint, decision any) (*interpreter.GateAnswer, error) {
	if cp == nil {
		return nil, &DecisionError{Msg: fmt.Sprintf("no suspended run of %q to resume", pipeline.Name)}
	}
	if cp.Gate == nil {
		return nil, &DecisionError{Msg: fmt.Sprintf("the run of %q is not waiting for a decision (it stopped at %q without `pause(..., decision: T)`) — resume it without one", pipeline.Name, cp.NextStep)}
	}
	t, ok := interpreter.ResolveDecisionType(prog, cp.Gate.Type)
	if !ok {
		return nil, &DecisionError{Msg: fmt.Sprintf("the gate at %q waits for an unknown type %q", cp.Gate.Step, cp.Gate.Type)}
	}
	v, err := coerceOne(cp.Gate.Type, t, decision, pipeline.Enums)
	if err != nil {
		return nil, &DecisionError{Msg: err.Error()}
	}
	replay, err := replayOf(prog, pipeline, cp.Gate)
	if err != nil {
		return nil, err
	}
	return &interpreter.GateAnswer{Step: cp.Gate.Step, Index: cp.Gate.Index, Value: v, Replay: replay}, nil
}

// replayOf checks the answers gate's step gave its earlier gates, as stored
// in the checkpoint, against their types again — so re-entering the step
// they are returned exactly as the first time.
func replayOf(prog *ast.Program, pipeline runtime.Pipeline, gate *runtime.Gate) ([]interpreter.GateReplay, error) {
	out := make([]interpreter.GateReplay, 0, len(gate.Answered))
	for i, r := range gate.Answered {
		t, ok := interpreter.ResolveDecisionType(prog, r.Type)
		if !ok {
			return nil, &DecisionError{Msg: fmt.Sprintf("gate %d of %q waited for an unknown type %q", i, gate.Step, r.Type)}
		}
		v, err := coerceOne(r.Type, t, r.Decision, pipeline.Enums)
		if err != nil {
			return nil, &DecisionError{Msg: fmt.Sprintf("gate %d of %q: %v", i, gate.Step, err)}
		}
		out = append(out, interpreter.GateReplay{Type: r.Type, Value: v})
	}
	return out, nil
}

// Run executes the pipeline described by req.
func Run(req Request) (*Result, error) {
	runCtx := req.Context
	if runCtx == nil {
		runCtx = context.Background()
	}
	if req.NoCache {
		runCtx = interpreter.WithoutAgentCache(runCtx)
	}
	// One prompt-rendering table per run, so an agent's prompt.target(t)
	// recognizes a variant prompt rendered in any earlier step.
	runCtx = interpreter.WithPromptRegistry(runCtx)
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
	// Also a lint error; checked here so a run never starts with a result
	// type it has no projection to check against.
	if pipeline.OutputType != nil && pipeline.Output == nil {
		return nil, fmt.Errorf("%s %q declares result type %s but no `output: { ... }` projection", pipeline.Kind, pipeline.Name, pipeline.OutputTypeName)
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
	if req.Decision != nil {
		switch {
		case !req.Resume:
			return nil, &DecisionError{Msg: "a decision answers a suspended run: resume it"}
		case pipeline.Loop:
			return nil, &DecisionError{Msg: fmt.Sprintf("%q is a loop %s: answering a gate on resume is not supported there yet", pipeline.Name, pipeline.Kind)}
		}
	}
	if req.From != "" {
		switch {
		case !req.Resume:
			return nil, &ResumeFromError{Msg: "redoing a stage resumes a stopped run: resume it"}
		case req.Decision != nil:
			return nil, &ResumeFromError{Msg: "a decision answers the gate the run waits at, while redoing a stage drops that gate — pass one or the other"}
		case pipeline.Loop:
			return nil, &ResumeFromError{Msg: fmt.Sprintf("%q is a loop %s: redoing a stage on resume is not supported there yet", pipeline.Name, pipeline.Kind)}
		}
		if _, ok := pipeline.StageEntry[req.From]; !ok {
			names := make([]string, 0, len(pipeline.StageEntry))
			for _, st := range pipeline.NamedStages() {
				names = append(names, st.Name)
			}
			msg := fmt.Sprintf("%s %q has no stage %q", pipeline.Kind, pipeline.Name, req.From)
			if len(names) > 0 {
				msg += " (its stages: " + strings.Join(names, ", ") + ")"
			} else {
				msg += " — it declares no stage"
			}
			return nil, &ResumeFromError{Msg: msg}
		}
	}

	baseStore := runtime.NewStore(base)
	sessionID := runtime.ResolveSession(baseStore, pipeline.Name, req.Session, req.Resume)

	// A decision answers the gate the suspended run waits at: checked
	// against it here, before anything (a hook included) runs, and handed to
	// that one gate, which consumes it.
	// So is a stage to redo: the stopped run must exist. And a run paused
	// on entering a stage whose inputs were unbound (InputGate) must now be
	// given them: checked before anything runs, so it stays paused if not.
	var answer *interpreter.GateAnswer
	// The checkpoint a resume continues from, if any: an input refused below
	// leaves it as it was.
	var resumeFrom *runtime.Checkpoint
	if req.Decision != nil || req.From != "" || (req.Resume && !pipeline.Loop) {
		peek := runtime.NewRunner(base).Session(sessionID)
		if req.StateStore != nil {
			peek.WithStateStore(req.StateStore)
		}
		cp, found, err := peek.Checkpoint(pipeline.Name)
		if err != nil {
			return nil, err
		}
		if !found {
			cp = nil
		}
		resumeFrom = cp
		if req.From != "" && cp == nil {
			return nil, &ResumeFromError{Msg: fmt.Sprintf("no stopped run of %q to redo stage %q in", pipeline.Name, req.From)}
		}
		if cp != nil && cp.InputGate != nil && req.From == "" {
			if req.Decision != nil {
				return nil, &DecisionError{Msg: fmt.Sprintf("the run of %q waits for the inputs of stage %q (%s), not a decision — resume it with them as inputs", pipeline.Name, cp.InputGate.Stage, strings.Join(cp.InputGate.Inputs, ", "))}
			}
			if err := pipeline.ValidateStageInputs(cp.InputGate.Stage, req.Inputs, cp.Variables); err != nil {
				return nil, err
			}
		}
		if req.Decision != nil {
			if answer, err = gateAnswer(prog, pipeline, cp, req.Decision); err != nil {
				return nil, err
			}
		}
	}
	if sessionID != "" {
		fmt.Fprintf(out, "session: %s\n", sessionID)
	}
	if req.OnStages != nil {
		req.OnStages(pipeline.NamedStages())
	}
	if req.OnStart != nil {
		req.OnStart(pipeline.Name, sessionID, append([]string(nil), pipeline.Steps...))
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
	coercedInputs = pipeline.WithBound(coercedInputs)
	if len(inputErrs) > 0 {
		var bad *runtime.InputValueError
		if errors.As(inputErrs[0], &bad) && resumeFrom != nil {
			bad.Resumable = true
			if resumeFrom.InputGate != nil {
				bad.Stage = resumeFrom.InputGate.Stage
			}
		}
		return nil, inputErrs[0]
	}

	memInit, err := interpreter.PipelineMemInit(prog, pipeline.Decl)
	if err != nil {
		return nil, err
	}

	// projectVars decides what a finished run exposes to the caller. With an
	// `output: { ... }` mapping declared, only those keys leave the run
	// (evaluated against the final variable state); without one, the legacy
	// behaviour returns every non-internal `var`. It is only ever called for a
	// terminal run — a paused run reports publicVars(partial state) instead,
	// since its `output:` expressions may read vars that are not set yet.
	//
	// A typed signature's result type (`workflow X(...): T`) is checked here
	// against the projection — on a normal finish and on `break` alike: a
	// caller gets exactly the shape the pipeline advertises, or an
	// *runtime.OutputContractError, never a partial result.
	projectVars := func(finalVars map[string]any) (map[string]any, error) {
		if pipeline.Output == nil {
			return publicVars(finalVars)
		}
		mem := memContextFor(memInit)
		projected, err := interpreter.EvalOutputs(runCtx, prog, pipeline.Output, file, out, store, jsonStore, mem, contextView, finalVars)
		if err != nil {
			return nil, err
		}
		if pipeline.OutputType != nil {
			if err := types.Check("output", *pipeline.OutputType, projected); err != nil {
				return nil, &runtime.OutputContractError{Pipeline: pipeline.Name, Type: pipeline.OutputTypeName, Err: err}
			}
		}
		return value.MaterializeVars(projected)
	}

	spawnSem := interpreter.NewSpawnSem(pipeline.Spawn.MaxConcurrency)

	// A parallel group's merge runs as one more step execution.
	stepTotal := len(pipeline.Steps)
	for _, st := range pipeline.Stages {
		if st.Merge != "" {
			stepTotal++
		}
	}
	var stepSeq int64

	// runHook evaluates one of the pipeline's optional lifecycle hooks —
	// session_start, session_end, step_start, step_end, stop_failure — a
	// no-op when the pipeline declares none. memVars is the run variable map
	// the hook's `mem` reads and writes (the step's own vars for step_start/
	// step_end, so a hook's mem write is the run's); nil gives it a scratch
	// map, where each mem reads as its initializer. hookOut is the writer the hook's log()/step
	// output goes to — callers running inside a `parallel` branch MUST pass
	// that branch's own stepOut, never the shared, run-wide out: a
	// step_start/step_end hook fired concurrently by sibling branches onto
	// one unsynchronized writer is a data race (confirmed by
	// TestPipelineStepHooksFireForEachParallelBranch under -race).
	runHook := func(hookExpr *ast.Expr, hookName string, arg map[string]any, memVars, vars map[string]any, hookOut io.Writer) error {
		if hookExpr == nil {
			return nil
		}
		mem := memContextFor(memInit).Bind(memVars)
		return interpreter.RunPipelineHook(runCtx, prog, pipeline.Decl, hookName, hookExpr, arg, file, hookOut, store, jsonStore, mem, contextView, vars)
	}

	// stopFailure fires the pipeline's stop_failure hook (if any) for a
	// genuine terminal failure — reason comes from *runtime.StepError.Kind
	// (the closed set "failed" | "timeout" | "cancelled" | "max_step_visits"),
	// or "runtime_error" for anything else that isn't step-shaped (a
	// checkpoint I/O error, an `output:`/context persistence failure). It
	// always returns err unchanged: a hook error here is only ever logged,
	// never allowed to mask the failure that triggered it.
	//
	// Its `mem` is the run's as the failure left it (rootVars: the run's own
	// vars, as last handed to a step) — what a resume would roll back to the
	// last completed step.
	var rootVars map[string]any
	stopFailure := func(err error) error {
		arg := map[string]any{"pipeline": pipeline.Name, "kind": pipeline.Kind, "step": "", "reason": "runtime_error", "error": err.Error()}
		var stepErr *runtime.StepError
		if errors.As(err, &stepErr) {
			arg["step"] = stepErr.Step
			arg["reason"] = stepErr.Kind
			arg["error"] = stepErr.Unwrap().Error()
		}
		if hookErr := runHook(pipeline.StopFailure, "stop_failure", arg, rootVars, nil, out); hookErr != nil {
			fmt.Fprintf(out, "warning: %s.stop_failure: %v\n", pipeline.Name, hookErr)
		}
		return err
	}

	exec := func(stepCtx context.Context, step string, ctx *runtime.RunContext) error {
		pipeline.BindInputs(ctx.Vars, coercedInputs)
		ctx.Vars["__last_step"] = step
		if !ctx.Branch {
			rootVars = ctx.Vars
		}
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
			"stage": pipeline.StageNameOf(step),
		}, ctx.Vars, ctx.Vars, stepOut); err != nil {
			return err
		}
		mem := memContextFor(memInit)
		stepStarted := time.Now()
		if answer != nil && answer.Step == step && !answer.Used {
			stepCtx = interpreter.WithGateAnswer(stepCtx, answer)
		}
		stepErr := interpreter.RunStep(stepCtx, prog, pipeline.Decl, step, file, stepOut, store, jsonStore, ctx.Vars, mem, contextView, spawnSem)
		breakReason, isBreak := interpreter.IsBreak(stepErr)
		pauseReason, isPause := interpreter.IsPause(stepErr)
		isComplete := interpreter.IsComplete(stepErr)
		gotoTarget, isGoto := interpreter.IsGoto(stepErr)
		if req.OnStepEnd != nil {
			status, target, endErr := "ok", "", error(nil)
			switch {
			case isBreak:
				status = "break"
			case isPause:
				status = "pause"
			case isComplete:
				status = "complete"
			case isGoto:
				status, target = "goto", gotoTarget
			case stepErr != nil:
				status, endErr = "failed", stepErr
			}
			req.OnStepEnd(step, index, status, target, endErr, time.Since(stepStarted))
		}
		if pipeline.StepEnd != nil {
			var hookErrVal any
			if stepErr != nil && !isBreak && !isPause && !isComplete && !isGoto {
				hookErrVal = stepErr.Error()
			}
			if hookErr := runHook(pipeline.StepEnd, "step_end", map[string]any{
				"pipeline": pipeline.Name, "step": step, "index": float64(index), "total": float64(stepTotal), "error": hookErrVal,
				"stage": pipeline.StageNameOf(step),
			}, ctx.Vars, ctx.Vars, stepOut); hookErr != nil {
				return hookErr
			}
		}
		switch {
		case isBreak:
			return &runtime.BreakSignal{Reason: breakReason}
		case isPause:
			return &runtime.PauseSignal{Reason: pauseReason, Gate: interpreter.PauseGate(stepErr)}
		case isComplete:
			return &runtime.CompleteSignal{}
		case isGoto:
			return &runtime.GotoSignal{Target: gotoTarget}
		default:
			return stepErr
		}
	}

	init := pipelineVarsInit(prog, pipeline.Decl, file, out, store, jsonStore, contextView)
	// A parallel group's own vars are seeded as it starts (each time it does).
	pipeline.GroupVarsInit = func(goctx context.Context, group string, rc *runtime.RunContext) error {
		groupOut := out
		if rc.Out != nil {
			groupOut = rc.Out
		}
		return interpreter.EvalGroupVars(goctx, prog, pipeline.Decl, group, file, groupOut, store, jsonStore, rc.Vars, memContextFor(memInit), contextView)
	}

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
	//
	// The hook reads the `mem` the run starts from: the stopped run's when
	// resuming (its checkpoint, or the loop's), each initializer otherwise.
	// It reads a copy: a mem write there does not reach the run.
	var startMem map[string]any
	if resumeFrom != nil {
		startMem = maps.Clone(runtime.MemVars(resumeFrom.Variables))
	}
	if pipeline.Loop {
		lr := runtime.NewLoopRunner(base).Session(sessionID)
		lr.Runner.WithDefinition(defDigest).WithForceResume(req.ForceResume)
		if req.LoopStateStore != nil {
			lr.WithLoopStateStore(req.LoopStateStore)
		}
		id, mem, _, err := lr.ResolveInstanceID(pipeline.Name, req.Resume)
		if err != nil {
			return nil, err
		}
		pipeline.InstanceID = id
		startMem = maps.Clone(mem)
	}
	// Every SessionContext field is emitted on every firing (nil where this
	// one has nothing to say) so the single declared SessionContext type
	// (types.go) — shared by session_start and session_end alike — always
	// structurally matches; see the field's own doc comment there.
	if err := runHook(pipeline.SessionStart, "session_start", map[string]any{
		"pipeline": pipeline.Name, "kind": pipeline.Kind, "session_id": sessionID,
		"resumed": req.Resume, "inputs": coercedInputs,
		"vars": nil, "broke": nil, "break_reason": nil, "iterations": nil,
	}, startMem, nil, out); err != nil {
		return nil, err
	}

	if !pipeline.Loop {
		runner := runtime.NewRunner(base).Session(sessionID).
			WithDefinition(defDigest).WithForceResume(req.ForceResume).WithResumeFrom(req.From).
			WithInputs(coercedInputs)
		runner.Out = out
		resultSink := runtime.StateStore(runner.Store)
		if req.StateStore != nil {
			runner.WithStateStore(req.StateStore)
			resultSink = req.StateStore
		}
		res, err := runner.Run(runCtx, pipeline, init, exec, req.Resume)
		if err != nil {
			return nil, stopFailure(err)
		}
		// A paused run is suspended, not finished — don't overwrite the
		// session's "last completed run" result.json for a `context:` reader,
		// and don't run the `output:` projection: its expressions may read a
		// var the run has not reached yet. Surface the partial state; the
		// resume that completes the run evaluates the real projection. Same
		// reasoning excludes it from session_end/stop_failure: a paused run
		// isn't finished, in either direction.
		vars := pausedVars(res.FinalVars)
		if !res.Paused {
			if err := persistContextResult(resultSink, pipeline, res.FinalVars); err != nil {
				return nil, stopFailure(err)
			}
			if vars, err = projectVars(res.FinalVars); err != nil {
				return nil, stopFailure(err)
			}
			if err := runHook(pipeline.SessionEnd, "session_end", map[string]any{
				"pipeline": pipeline.Name, "kind": pipeline.Kind, "session_id": sessionID,
				"resumed": nil, "inputs": nil,
				"vars": vars, "broke": res.Broke, "break_reason": res.BreakReason, "iterations": nil,
			}, res.FinalVars, res.FinalVars, out); err != nil {
				return nil, err
			}
		}
		return &Result{
			PipelineName:   pipeline.Name,
			SessionID:      sessionID,
			Executed:       res.Executed,
			Skipped:        res.Skipped,
			Resumed:        res.Resumed,
			Broke:          res.Broke,
			BreakReason:    res.BreakReason,
			Paused:         res.Paused,
			PauseReason:    res.PauseReason,
			Awaiting:       awaitingOf(prog, pipeline, res.PauseGate),
			AwaitingInputs: awaitingInputsOf(pipeline, res.PauseInputs),
			Vars:           vars,
		}, nil
	}

	evalStopWhen := func(vars map[string]any) (bool, error) {
		if pipeline.StopWhen == nil {
			return false, nil
		}
		mem := memContextFor(memInit).Bind(vars)
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
		return nil, stopFailure(err)
	}
	paused := res.TerminalReason == "pause"
	// As in the non-loop path: a paused loop reports its partial state, not the
	// `output:` projection, which is evaluated only once the run terminates.
	vars := pausedVars(res.FinalVars)
	if !paused {
		if err := persistContextResult(loopResultSink, pipeline, res.FinalVars); err != nil {
			return nil, stopFailure(err)
		}
		if vars, err = projectVars(res.FinalVars); err != nil {
			return nil, stopFailure(err)
		}
		if err := runHook(pipeline.SessionEnd, "session_end", map[string]any{
			"pipeline": pipeline.Name, "kind": pipeline.Kind, "session_id": sessionID,
			"resumed": nil, "inputs": nil,
			"vars": vars, "broke": res.TerminalReason == "break", "break_reason": res.BreakReason,
			"iterations": float64(res.Iterations),
		}, res.FinalVars, res.FinalVars, out); err != nil {
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
// only the pipeline's declared inputs and vars. Returns nil for nil. Ref
// objects are materialized (value.MaterializeVars): a result leaves the
// interpreter, so it is plain data — and RedactVars, which walks plain
// maps, can then mask a secret held inside one. A ref cycle has no plain
// form and is an error.
func publicVars(vars map[string]any) (map[string]any, error) {
	if vars == nil {
		return nil, nil
	}
	clean := make(map[string]any, len(vars))
	for k, v := range vars {
		if strings.HasPrefix(k, "__") {
			continue
		}
		clean[k] = v
	}
	return value.MaterializeVars(clean)
}

// pausedVars is the display-only view of a paused run's partial state:
// publicVars, except a ref cycle is shown as "[circular ref]" rather than
// failing — a pause isn't a result, and the checkpoint keeps the cycle.
func pausedVars(vars map[string]any) map[string]any {
	if vars == nil {
		return nil
	}
	clean := make(map[string]any, len(vars))
	for k, v := range vars {
		if !strings.HasPrefix(k, "__") {
			clean[k] = value.MaterializeLossy(v)
		}
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
	clean, err := publicVars(finalVars)
	if err != nil {
		return err
	}
	return store.WriteResult(pipeline.Name, clean)
}

// memContextFor builds the interpreter.MemContext for a pipeline's `mem`
// declarations, or nil when the pipeline declares none. It holds no values:
// each call it is handed to binds it to the run variables it works on
// (interpreter.MemContext.Bind), where every `mem` lives under
// runtime.MemVarPrefix. So `mem` is the run's state, kept exactly where the
// checkpoint is — the session directory, or an `extension state` shared by
// replicas — isolated per session, resumed with `--resume`, rolled back with
// a failed step, gone with a throwaway BaseDir (a synchronous MCP
// tools/call), and carried across a loop's iterations (runtime.LoopRunner).
func memContextFor(init map[string]*ast.Expr) *interpreter.MemContext {
	if len(init) == 0 {
		return nil
	}
	return &interpreter.MemContext{Init: init}
}
