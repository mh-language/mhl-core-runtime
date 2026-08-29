package interpreter

import (
	"fmt"
	"io"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// EvalCondition evaluates expr — a `loop`'s stop_when — against a fresh
// environment backed by the same memory stores a step would use. It exists
// because a loop's stop condition isn't inside any step body: it's
// re-checked by runtime.LoopRunner once a full pipeline iteration finishes,
// so there's no *ast.Step for RunStep to run it as part of. Like a step's
// own `var`, nothing declared while evaluating expr persists anywhere, and
// a pipeline `var` is not visible here either (pipelineEnv is intentionally
// left unset) — state a stop_when needs to observe across iterations is
// expected to go through `mem` or `memory`, not `var`; see
// checkLoopStopWhen (internal/lang/lint/loop.go), which flags a `var`
// reference here at lint time. mem (may be nil) is the one exception: a
// pipeline's `mem` declarations ARE visible here, since surviving
// stop_when's own re-checks across iterations is `mem`'s entire purpose.
// cctx (may be nil) is likewise visible here, so a stop_when can read
// `context.*` — see ContextView.
func EvalCondition(prog *ast.Program, expr *ast.Expr, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore, mem *MemContext, cctx *ContextView) (bool, error) {
	ctx := &evalCtx{prog: prog, store: store, jsonStore: jsonStore, out: out, env: Env{}, mem: mem, cctx: cctx, file: file, aliasTypes: aliasTypesFor(prog)}
	v, err := evalExpr(ctx, expr)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("stop_when must evaluate to a bool, got %s", typeName(v))
	}
	return b, nil
}
