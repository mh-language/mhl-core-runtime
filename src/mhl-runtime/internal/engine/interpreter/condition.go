package interpreter

import (
	"fmt"
	"io"

	"github.com/yanjustino/mhl-runtime/internal/features/memory"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

// EvalCondition evaluates expr — a `loop`'s stop_when — against a fresh
// environment backed by the same memory stores a step would use. It exists
// because a loop's stop condition isn't inside any step body: it's
// re-checked by runtime.LoopRunner once a full pipeline iteration finishes,
// so there's no *ast.Step for RunStep to run it as part of. Like a step's
// own `var`, nothing declared while evaluating expr persists anywhere —
// state a stop_when needs to observe across iterations is expected to go
// through `memory`, exactly as a step's would.
func EvalCondition(prog *ast.Program, expr *ast.Expr, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore) (bool, error) {
	ctx := &evalCtx{prog: prog, store: store, jsonStore: jsonStore, out: out, env: Env{}, file: file}
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
