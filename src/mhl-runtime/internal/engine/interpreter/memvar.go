package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/engine/value"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// MemContext gives one run access to its pipeline's `mem` declarations
// (PipelineMember.Mem, ast/pipeline.go). Init holds each declared name's
// get-or-init initializer expression, evaluated the first time the run reads
// or writes a name that has no value yet. Vars is the run variable map the
// values live in, each under runtime.MemVarKey(name): the run's own
// RunContext.Vars (or a parallel branch's copy of it), so a `mem` is
// checkpointed, resumed, rolled back on a failed step and merged after a
// `parallel` group exactly like a `var` — and goes to the same place, the
// session directory or an `extension state` — while a loop carries it into
// its next iteration (runtime.LoopRunner). Nil (not just empty) wherever a
// pipeline declares no `mem` at all; isMemVar treats a nil ctx.mem the same
// as an empty Init.
//
// A MemContext built without Vars (execsvc's memContextFor, the test runner)
// is bound to the map of the call it is handed to — RunStep and
// EvalGroupVars to the run's vars, RunPipelineHook and EvalOutputs to the
// vars they are given — through Bind.
type MemContext struct {
	Init map[string]*ast.Expr
	Vars map[string]any
}

// Bind returns m reading and writing its values in vars. A MemContext that
// already has Vars is returned unchanged, and a nil one stays nil. With vars
// nil the values go to a scratch map nothing reads afterwards: the hook or
// expression still sees each `mem` (its initializer), but a write is
// dropped.
func (m *MemContext) Bind(vars map[string]any) *MemContext {
	if m == nil || m.Vars != nil {
		return m
	}
	if vars == nil {
		vars = map[string]any{}
	}
	return &MemContext{Init: m.Init, Vars: vars}
}

// snapshot returns a copy of m whose values are a deep copy of m's: what a
// spawned agent call (spawn.go) reads, so it never races the step that
// spawned it — and, like a `var` there, a write it makes stays its own.
func (m *MemContext) snapshot() *MemContext {
	if m == nil {
		return nil
	}
	vars := map[string]any{}
	for k, v := range runtime.MemVars(m.Vars) {
		vars[k] = value.DeepCopy(v)
	}
	return &MemContext{Init: m.Init, Vars: vars}
}

// isMemVar reports whether name was declared with `mem` in the pipeline
// currently executing — the third and last tier a bare identifier read
// (evalPrimary) or assignment (execAssign) falls back to, after ctx.env and
// ctx.pipelineEnv.
func isMemVar(ctx *evalCtx, name string) bool {
	if ctx.mem == nil {
		return false
	}
	_, ok := ctx.mem.Init[name]
	return ok
}

// readMemVar returns name's current value, running its get-or-init
// initializer (and storing the result) the first time this run reads or
// writes it. The value is a copy, like any variable read: names never alias.
func readMemVar(ctx *evalCtx, name string) (any, error) {
	if ctx.mem.Vars == nil {
		ctx.mem = ctx.mem.Bind(nil)
	}
	if v, ok := ctx.mem.Vars[runtime.MemVarKey(name)]; ok {
		return value.DeepCopy(v), nil
	}
	initVal, err := evalExpr(ctx, ctx.mem.Init[name])
	if err != nil {
		return nil, err
	}
	if err := writeMemVar(ctx, name, initVal); err != nil {
		return nil, err
	}
	return value.DeepCopy(initVal), nil
}

// writeMemVar stores value under name, overwriting whatever get-or-init
// would otherwise have produced.
func writeMemVar(ctx *evalCtx, name string, v any) error {
	// A `mem` is persisted JSON: a ref object is stored as the plain object
	// it holds (its identity doesn't survive there).
	plain, err := value.Materialize(v)
	if err != nil {
		return fmt.Errorf("mem %q: %w", name, err)
	}
	if ctx.mem.Vars == nil {
		ctx.mem = ctx.mem.Bind(nil)
	}
	ctx.mem.Vars[runtime.MemVarKey(name)] = plain
	return nil
}

// resetMemVar deletes name's stored value, so the next read or write
// re-runs its get-or-init initializer — this is what `count.reset()`
// (evalPostfix) compiles down to.
func resetMemVar(ctx *evalCtx, name string) error {
	if ctx.mem.Vars != nil {
		delete(ctx.mem.Vars, runtime.MemVarKey(name))
	}
	return nil
}

// execMemAssign handles `name = expr` (and `name[i] = expr`, `name.f =
// expr`) once execAssign has determined name is a `mem` var, not an env or
// pipelineEnv one. A plain assignment (no index/member trailers) just
// writes through; an indexed one reads the current container (running
// get-or-init if this is the first touch), mutates it in place the same way
// applyTrailers/indexWrite already do for ctx.env (a Go map/slice shares
// backing storage with every alias), then writes the whole mutated
// container back (readMemVar hands out a copy, so the mutation alone would
// not reach the stored value).
// ops is the index-chain to apply on top of name — assign.Target.Ops for an
// ordinary `pending["key"] = value` target, or nil for a `self.name = value`
// target (assignTargetBase restricts self to a whole-value assignment, so
// execAssign always passes nil there regardless of assign.Target.Ops, which
// for that shape is the unrelated `.name` member trailer, not an index
// chain).
func execMemAssign(ctx *evalCtx, name string, ops []*ast.Trailer, assign *ast.AssignStmt) error {
	v, err := evalExpr(ctx, assign.Value)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		if assign.Op == "+=" {
			cur, err := readMemVar(ctx, name)
			if err != nil {
				return err
			}
			if v, err = addValues(cur, v); err != nil {
				return err
			}
		}
		return writeMemVar(ctx, name, v)
	}
	root, err := readMemVar(ctx, name)
	if err != nil {
		return err
	}
	if assign.Op == "+=" {
		cur, err := applyTrailers(ctx, root, ops, 0)
		if err != nil {
			return err
		}
		if v, err = addValues(cur, v); err != nil {
			return err
		}
	}
	container, err := applyTrailers(ctx, root, ops[:len(ops)-1], 0)
	if err != nil {
		return err
	}
	if err := indexWrite(ctx, container, ops[len(ops)-1].Index, v, 0); err != nil {
		return err
	}
	return writeMemVar(ctx, name, root)
}

// PipelineMemInit collects pipelineName's `mem` declarations (in source
// order) into a name->initializer map — the lint-free run-time twin of
// EvalPipelineVars, except nothing is evaluated here: a mem initializer
// only ever runs lazily, once per run, from readMemVar's get-or-init check. Returns a nil map (not an error) for a pipeline with
// no `mem` declarations, so a caller can pass a nil *MemContext straight
// through to RunStep/EvalCondition for the common case.
func PipelineMemInit(prog *ast.Program, pipelineName string) (map[string]*ast.Expr, error) {
	var pipeline *ast.Pipeline
	for _, decl := range prog.Decls {
		if decl.Pipeline != nil && decl.Pipeline.Name == pipelineName {
			pipeline = decl.Pipeline
			break
		}
	}
	if pipeline == nil {
		return nil, fmt.Errorf("pipeline %q not found", pipelineName)
	}

	var init map[string]*ast.Expr
	for _, member := range pipeline.Body {
		if member.Mem == nil {
			continue
		}
		if init == nil {
			init = map[string]*ast.Expr{}
		}
		init[member.Mem.Name] = member.Mem.Value
	}
	return init, nil
}
