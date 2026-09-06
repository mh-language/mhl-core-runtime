package interpreter

import (
	"context"
	"fmt"
	"io"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// EvalOutputs evaluates a workflow's `output: { name: expr, ... }` mapping
// against its final variable state and returns the resulting object. It is
// the projection that decides what a run actually exposes to a caller
// (execsvc.Result.Vars, and through it the MCP / A2A reply): with an
// `output:` block declared, only these keys leave the run — never the full
// set of internal `var`s.
//
// It runs once, after the run reaches a terminal state (a completion or a
// `break`) — never for a paused run, whose vars may not be populated yet — and
// its result is not checkpointed: a `--resume` that finishes the run
// re-evaluates it. goctx is the run's context, so a mapping expression that
// blocks (a native op, an agent call) observes the run's cancellation and
// deadline. Side effects in a mapping expression are discouraged for the same
// reasons: run-once, not persisted, re-run on resume.
//
// vars is the run's final variable environment; a mapping expression reads it
// by bare identifier exactly as a step body would. mem and cctx (either may
// be nil) make a pipeline's `mem` declarations and `context.*` readable too,
// matching EvalCondition. Nothing declared while evaluating the mapping
// persists anywhere.
func EvalOutputs(goctx context.Context, prog *ast.Program, expr *ast.Expr, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore, mem *MemContext, cctx *ContextView, vars map[string]any) (map[string]any, error) {
	env := make(Env, len(vars))
	for k, v := range vars {
		env[k] = v
	}
	ctx := &evalCtx{
		prog:       prog,
		store:      store,
		jsonStore:  jsonStore,
		out:        out,
		env:        env,
		mem:        mem,
		cctx:       cctx,
		file:       file,
		goctx:      goctx,
		aliasTypes: aliasTypesFor(prog),
	}
	v, err := evalExpr(ctx, expr)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("output must be an object mapping, got %s", typeName(v))
	}
	return obj, nil
}
