package interpreter

// ContextView is the read-only data a pipeline's `context:` element exposes
// to its steps as the identifier `context`: this execution's session
// metadata, plus the variable state carried over from a prior run (see
// runtime.ContextConfig.Source — the most recent completed run's result.json,
// or a still-present checkpoint). It is assembled by cli.go, the one place
// the interpreter and the runtime's session machinery are both in scope, and
// threaded through RunStep/EvalCondition/EvalPipelineVars exactly like
// MemContext — nil (not just empty) wherever the pipeline declares no
// `context:` block, so a bare `context` stays an undefined identifier there.
//
// Deliberately holds no runtime import: cli.go copies the plain values in.
type ContextView struct {
	SessionID string
	StartedAt string
	Resumed   bool
	Vars      map[string]any
}

// contextSnapshot renders a ContextView as the plain map that `context`
// resolves to, so the existing member (`context.session_id`) and index
// (`context.vars["k"]`) trailers work against it with no new dispatch code.
func contextSnapshot(c *ContextView) map[string]any {
	vars := c.Vars
	if vars == nil {
		vars = map[string]any{}
	}
	return map[string]any{
		"session_id": c.SessionID,
		"started_at": c.StartedAt,
		"resumed":    c.Resumed,
		"vars":       vars,
	}
}

// isContextRef reports whether name is the `context` identifier in a pipeline
// that declared a `context:` block — the fourth and last fallback tier a
// bare identifier read falls back to, after env, pipelineEnv and mem. Unlike
// those, it is read-only: execAssign rejects an assignment to it.
func isContextRef(ctx *evalCtx, name string) bool {
	return ctx.cctx != nil && name == "context"
}
