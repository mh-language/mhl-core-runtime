package interpreter

import (
	"context"
	"fmt"
	"io"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// RunPipelineHook evaluates one of a pipeline/workflow's five lifecycle
// hooks — session_start, session_end, step_start, step_end, stop_failure —
// each an ordinary `name: (x) -> { ... }` body Property (ast.PipelineMember.Prop,
// no grammar of its own; see ast.PipelineBodyProperties). It follows the
// "build a fresh root evalCtx and evaluate" shape EvalOutputs/EvalCondition
// already use for a pipeline-level *ast.Expr — not RunStep's, which resolves
// a step by name — since runtime.Pipeline already hands the hook's raw
// *ast.Expr straight from PipelineFromAST, exactly like Output does.
//
// Unlike agent before/after (agent_hooks.go, zero parameters — the calling
// step's env is already in scope, so nothing needs passing in) a pipeline
// hook takes exactly one positional parameter, like system_prompt's two:
// there is no step-local env to fall back on here, so the payload (a
// SessionContext/StepContext/FailureContext-shaped map — see execsvc.go's
// callers) has to arrive as an explicit argument. vars seeds env the same
// way EvalOutputs does, so a hook body can also read pipeline vars by bare
// identifier in addition to its named parameter.
//
// Hooks are observation-only: a non-nil return is a runtime error, not a
// value execsvc does anything with. Any error — the hook expression isn't a
// 1-param lambda, the call itself fails, or it returns something — is
// wrapped with pipelineName/hookName and handed back for the caller to
// decide how to treat (execsvc.go: fatal for every hook except stop_failure,
// where it's appended to, never replaces, the failure being reported).
func RunPipelineHook(goctx context.Context, prog *ast.Program, pipelineName, hookName string, expr *ast.Expr, arg any, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore, mem *MemContext, cctx *ContextView, vars map[string]any) error {
	env := make(Env, len(vars))
	for k, v := range vars {
		env[k] = v
	}
	ctx := &evalCtx{
		prog:         prog,
		pipelineName: pipelineName,
		store:        store,
		jsonStore:    jsonStore,
		out:          out,
		env:          env,
		mem:          mem,
		cctx:         cctx,
		file:         file,
		goctx:        goctx,
		aliasTypes:   aliasTypesFor(prog),
	}
	v, err := evalExpr(ctx, expr)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", pipelineName, hookName, err)
	}
	closure, ok := v.(*Closure)
	if !ok {
		return fmt.Errorf("%s.%s must be a lambda, e.g. (x) -> { ... }", pipelineName, hookName)
	}
	if len(closure.def.Params) != 1 {
		return fmt.Errorf("%s.%s takes exactly one parameter, got %d", pipelineName, hookName, len(closure.def.Params))
	}
	result, err := invokeClosureWithValues(closure, []any{arg}, 0)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", pipelineName, hookName, err)
	}
	if result != nil {
		return fmt.Errorf("%s.%s must not return a value (lifecycle hooks are for side effects only), got %s", pipelineName, hookName, typeName(result))
	}
	return nil
}
