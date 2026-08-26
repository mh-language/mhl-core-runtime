package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// MemContext is the persistent backing for one pipeline instance's `mem`
// declarations (PipelineMember.Mem, ast/pipeline.go): Path is the JSON file
// (via ctx.jsonStore) holding this instance's values, one key per mem
// variable name; Init holds each declared name's get-or-init initializer
// expression, evaluated at most once per Path — the first read or write of
// a name whose key isn't in the file yet. Nil (not just empty) wherever a
// pipeline declares no `mem` at all, so RunStep/EvalCondition callers don't
// need to special-case "no mem" themselves; isMemVar treats a nil ctx.mem
// the same as an empty Init.
//
// Unlike pipelineEnv (a step's own `var`s), Path is stable across every
// `Runner.Run()` call of one `mhl run` invocation of this pipeline — every
// loop iteration reuses it — which is what lets a `mem` counter survive
// across iterations where a `var` counter resets; see cli.go for how Path
// is derived from the pipeline's checkpoint config and instance id.
type MemContext struct {
	Path string
	Init map[string]*ast.Expr
}

// memUnset is a private sentinel passed as JSONStore.Get's default, so
// readMemVar can tell "this key was never Set" (get-or-init should run)
// apart from a legitimately stored JSON null (get-or-init already ran once
// and the value happens to be nil).
type memUnsetType struct{}

var memUnset = memUnsetType{}

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

// readMemVar returns name's current value from ctx.mem's backing store,
// running its get-or-init initializer (and persisting the result) the first
// time this pipeline instance ever reads or writes it.
func readMemVar(ctx *evalCtx, name string) (any, error) {
	v, err := ctx.jsonStore.Get(ctx.mem.Path, name, memUnset)
	if err != nil {
		return nil, err
	}
	if _, unset := v.(memUnsetType); !unset {
		return v, nil
	}
	initVal, err := evalExpr(ctx, ctx.mem.Init[name])
	if err != nil {
		return nil, err
	}
	if err := writeMemVar(ctx, name, initVal); err != nil {
		return nil, err
	}
	return initVal, nil
}

// writeMemVar stores value under name in ctx.mem's backing store,
// overwriting whatever get-or-init would otherwise have produced.
func writeMemVar(ctx *evalCtx, name string, value any) error {
	return ctx.jsonStore.Set(ctx.mem.Path, name, value)
}

// resetMemVar deletes name's stored value, so the next read or write
// re-runs its get-or-init initializer — this is what `count.reset()`
// (evalPostfix) compiles down to.
func resetMemVar(ctx *evalCtx, name string) error {
	_, err := ctx.jsonStore.Remove(ctx.mem.Path, name)
	return err
}

// execMemAssign handles `name = expr` (and `name[i] = expr`, `name.f =
// expr`) once execAssign has determined name is a `mem` var, not an env or
// pipelineEnv one. A plain assignment (no index/member trailers) just
// writes through; an indexed one reads the current container (running
// get-or-init if this is the first touch), mutates it in place the same way
// applyTrailers/indexWrite already do for ctx.env (a Go map/slice shares
// backing storage with every alias), then writes the whole mutated
// container back — the in-place mutation alone would update JSONStore's
// in-memory cache but never reach disk without this explicit write.
func execMemAssign(ctx *evalCtx, name string, assign *ast.AssignStmt) error {
	v, err := evalExpr(ctx, assign.Value)
	if err != nil {
		return err
	}
	ops := assign.Target.Ops
	if len(ops) == 0 {
		return writeMemVar(ctx, name, v)
	}
	root, err := readMemVar(ctx, name)
	if err != nil {
		return err
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
// only ever runs lazily, once per backing file, from readMemVar's
// get-or-init check. Returns a nil map (not an error) for a pipeline with
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
