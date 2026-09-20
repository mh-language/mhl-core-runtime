package interpreter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// findPipelineDecl locates a top-level `pipeline`/`workflow` declaration by
// name — this file's own small copy of the same lookup every other file
// that needs one (exec.go's EvalPipelineVars, memvar.go's PipelineMemInit,
// nameof.go, imports.go) already keeps independently; there is no single
// shared source for it.
func findPipelineDecl(prog *ast.Program, name string) (*ast.Pipeline, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Pipeline != nil && decl.Pipeline.Name == name {
			return decl.Pipeline, true
		}
	}
	return nil, false
}

// runWorkflowCall handles `Name.run(inputs: {...})` where Name resolves to a
// declared `pipeline`/`workflow` (MHL-Melhorias.md #13) — the test-only
// counterpart to an agent's `Name.run(prompt: ...)`, reached from the same
// `case member == "run":` tier in evalPostfix once findAgent has already
// missed. Restricted to a describe block's body (ctx.assertions != nil):
// running a whole pipeline — its own steps, checkpointing, `goto`, `pause`
// — from inside an already-running step would mean one run executing
// another synchronously mid-step, with no answer yet for what that should
// do to checkpointing, `context`, or recursion depth. Nothing stops that
// design question from being revisited later; today it's just out of scope
// for "let a test exercise a workflow's control flow."
func runWorkflowCall(ctx *evalCtx, name string, decl *ast.Pipeline, call *ast.Call, depth int) (any, error) {
	if ctx.assertions == nil {
		return nil, fmt.Errorf("%s.run(...) targets a pipeline/workflow, which can only be run from inside a test's describe block", name)
	}
	inputs, err := resolveInputsArg(ctx, call, depth)
	if err != nil {
		return nil, fmt.Errorf("%s.run: %w", name, err)
	}
	// decl.Name, not name: name may be an import alias (findPipelineDecl
	// resolved it via resolveName to find decl), but runtime.FindPipeline
	// and every other lookup below works off the program's own decls and
	// knows nothing of import aliases.
	return runWorkflowForTest(ctx, decl.Name, inputs, depth)
}

// resolveInputsArg reads call's `inputs:` named argument as an object —
// defaulting to {} when absent, so a pipeline/workflow with no declared
// inputs (or every input defaulted) can be run as `Name.run()`.
func resolveInputsArg(ctx *evalCtx, call *ast.Call, depth int) (map[string]any, error) {
	for _, arg := range call.Args {
		if arg.Name != "inputs" {
			continue
		}
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return nil, err
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("inputs must be an object, got %s", typeName(v))
		}
		return obj, nil
	}
	return map[string]any{}, nil
}

// runWorkflowForTest runs name's declared pipeline/workflow to completion —
// or to a `break`/`pause`/failure — inside an isolated, throwaway sandbox: a
// temp directory for any incidental on-disk state (a `mem` variable) and an
// in-memory StateStore for checkpoints, both discarded when this returns, so
// running the same test twice never collides with itself or leaves state
// behind the way a real `mhl run` deliberately does. Reuses this package's
// own RunStep/EvalPipelineVars/PipelineMemInit — the same building blocks
// internal/execsvc.Run wires together for a real `mhl run` — rather than
// calling execsvc itself: execsvc already depends on this package (it wraps
// the interpreter), so the reverse import would be a cycle.
//
// Only a problem with the *call itself* — an unknown name, a `loop
// pipeline`/`loop workflow` (not supported yet), or an input that fails
// InputSchema/type validation — raises a catchable error. Everything the
// pipeline's own steps do — fail(), break, pause(), goto, a normal finish —
// is reported in the returned object instead, so a test can assert on a
// workflow that is *expected* to fail or break exactly as easily as one
// expected to complete: {ok, state, executed, vars, error, step,
// break_reason, pause_reason}. state is one of "completed", "paused",
// "broke", "failed". vars is {} on failure (RunResult.FinalVars is nil
// then — nothing was captured to report).
func runWorkflowForTest(ctx *evalCtx, name string, inputs map[string]any, depth int) (map[string]any, error) {
	pipeline, err := runtime.FindPipeline(ctx.prog, name)
	if err != nil {
		return nil, err
	}
	if pipeline.Loop {
		return nil, fmt.Errorf("%q: a `loop pipeline`/`loop workflow` cannot be run from a test yet — test its step bodies directly, or exercise it via `mhl run`", name)
	}
	if inputs == nil {
		inputs = map[string]any{}
	}
	if err := pipeline.ValidateInputs(inputs); err != nil {
		return nil, err
	}
	for _, in := range pipeline.Inputs {
		v, ok := inputs[in.Name]
		if !ok {
			continue
		}
		if err := types.Check(fmt.Sprintf("input %q", in.Name), in.Type, v); err != nil {
			return nil, err
		}
	}

	tmpDir, err := os.MkdirTemp("", "mhl-test-run-*")
	if err != nil {
		return nil, fmt.Errorf("%s.run: %w", name, err)
	}
	defer os.RemoveAll(tmpDir)

	memInit, err := PipelineMemInit(ctx.prog, pipeline.Name)
	if err != nil {
		return nil, err
	}
	var mem *MemContext
	if len(memInit) > 0 {
		mem = &MemContext{
			Path: filepath.Join(tmpDir, runtime.StateDirName, "mem", pipeline.Name, "default.json"),
			Init: memInit,
		}
	}

	sessionID := runtime.NewSessionID()
	contextView := &ContextView{SessionID: sessionID, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	spawnSem := NewSpawnSem(pipeline.Spawn.MaxConcurrency)

	exec := func(stepCtx context.Context, step string, rc *runtime.RunContext) error {
		for k, v := range inputs {
			rc.Vars[k] = v
		}
		stepErr := RunStep(stepCtx, ctx.prog, step, ctx.file, ctx.out, ctx.store, ctx.jsonStore, rc.Vars, mem, contextView, spawnSem)
		if reason, ok := IsBreak(stepErr); ok {
			return &runtime.BreakSignal{Reason: reason}
		}
		if reason, ok := IsPause(stepErr); ok {
			return &runtime.PauseSignal{Reason: reason}
		}
		if target, ok := IsGoto(stepErr); ok {
			return &runtime.GotoSignal{Target: target}
		}
		if IsComplete(stepErr) {
			return &runtime.CompleteSignal{}
		}
		return stepErr
	}
	init := func(rc *runtime.RunContext) error {
		env, err := EvalPipelineVars(ctx.prog, pipeline.Name, ctx.file, ctx.out, ctx.store, ctx.jsonStore, contextView)
		if err != nil {
			return err
		}
		for k, v := range env {
			rc.Vars[k] = v
		}
		return nil
	}

	runner := runtime.NewRunner(tmpDir).Session(sessionID).WithStateStore(newMemStateStore())
	res, runErr := runner.Run(goctxOf(ctx), pipeline, init, exec, false)

	out := map[string]any{
		"ok":           runErr == nil,
		"state":        "completed",
		"executed":     stringsToAny(res.Executed),
		"vars":         map[string]any{},
		"error":        "",
		"step":         "",
		"break_reason": nil,
		"pause_reason": nil,
	}
	if res.FinalVars != nil {
		out["vars"] = res.FinalVars
	}
	switch {
	case runErr != nil:
		out["ok"] = false
		out["state"] = "failed"
		out["error"] = runErr.Error()
		var stepErr *runtime.StepError
		if errors.As(runErr, &stepErr) {
			out["step"] = stepErr.Step
		}
	case res.Paused:
		out["state"] = "paused"
		out["pause_reason"] = res.PauseReason
	case res.Broke:
		out["state"] = "broke"
		out["break_reason"] = res.BreakReason
	}
	return out, nil
}

func stringsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// memStateStore is an in-memory runtime.StateStore — checkpoints and
// results live only for the duration of one runWorkflowForTest call and are
// never written to disk. Safe for concurrent use: a tested pipeline's
// `parallel` group runs its branch steps concurrently, and each may
// checkpoint on its own goroutine.
type memStateStore struct {
	mu          sync.Mutex
	checkpoints map[string]*runtime.Checkpoint
	results     map[string]map[string]any
}

func newMemStateStore() *memStateStore {
	return &memStateStore{
		checkpoints: map[string]*runtime.Checkpoint{},
		results:     map[string]map[string]any{},
	}
}

func (s *memStateStore) Load(pipeline string) (*runtime.Checkpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp, ok := s.checkpoints[pipeline]
	return cp, ok, nil
}

func (s *memStateStore) Save(cp *runtime.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[cp.Pipeline] = cp
	return nil
}

func (s *memStateStore) Clear(pipeline string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.checkpoints, pipeline)
	return nil
}

func (s *memStateStore) WriteResult(pipeline string, vars map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[pipeline] = vars
	return nil
}
