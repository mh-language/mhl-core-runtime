package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// agentHookExpr reads an agent's `before`/`after` property, if declared — an
// ordinary Property whose Value is a lambda literal (`() -> { ... }`), the
// same shape a `filter(items, (item) -> ...)` predicate already is; no new
// grammar is needed for this.
func agentHookExpr(agent *ast.Agent, name string) (*ast.Expr, bool) {
	for _, p := range agent.Props {
		if p.Name == name {
			return p.Value, true
		}
	}
	return nil, false
}

func evalHookClosure(ctx *evalCtx, agentName, hookName string, expr *ast.Expr, depth int) (*Closure, error) {
	v, err := evalExprAt(ctx, expr, depth)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", agentName, hookName, err)
	}
	closure, ok := v.(*Closure)
	if !ok {
		return nil, fmt.Errorf("%s.%s must be a lambda, e.g. () -> { ... }", agentName, hookName)
	}
	if len(closure.def.Params) != 0 {
		return nil, fmt.Errorf("%s.%s takes no parameters — write `() -> { ... }`; declared tools/extensions are called by name, and `after` reads the response as `result`", agentName, hookName)
	}
	return closure, nil
}

// runAgentBeforeHook evaluates an agent's `before: () -> {...}` hook, if
// declared, and returns the object it returned — nil, nil when the agent
// declares no `before` at all. The hook is an ordinary closure
// (newClosure/evalPrimary's Lambda case), so it captures ctx's env at the
// point runAgent calls this — the calling step's own variables are visible to
// it — and resolves declared `tool`/`extension`/`agent` names, native ops and
// `mem` exactly as any expression position does.
//
// mhl agents are CLI subprocesses with no structural tool-calling channel of
// their own, so `before` is the only way a pipeline can hand an agent real,
// already-fetched data instead of hoping the model reads and acts on a text
// instruction: its returned object's fields become named
// bindings runAgent makes available to the prompt's own "${...}"
// interpolation, so `before: () -> { return { repo: GitHub.call(...),
// readme: files.read("README.md") } }` really does make `${repo}`/`${readme}`
// resolve inside `prompt: "..."` — with real, already-executed values.
func runAgentBeforeHook(ctx *evalCtx, agentName string, agent *ast.Agent, depth int) (map[string]any, error) {
	expr, ok := agentHookExpr(agent, "before")
	if !ok {
		return nil, nil
	}
	closure, err := evalHookClosure(ctx, agentName, "before", expr, depth)
	if err != nil {
		return nil, err
	}
	result, err := invokeClosureWithValues(closure, nil, depth)
	if err != nil {
		return nil, fmt.Errorf("%s.before: %w", agentName, err)
	}
	if result == nil {
		return nil, nil
	}
	obj, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.before must return an object (or nothing), got %s", agentName, typeName(result))
	}
	return obj, nil
}

// runAgentAfterHook evaluates an agent's `after: () -> {...}` hook, if
// declared, against response — response unchanged, nil when the agent
// declares no `after`. The response is bound in the hook body as `result`. A
// non-nil string return replaces response as what `.run()` ultimately returns
// to the caller; any other return (including none) leaves response unchanged
// — `after` is for a side effect (log, persist, validate, chain another call)
// far more often than for rewriting the response, so passthrough is the
// default a hook doesn't have to opt into.
func runAgentAfterHook(ctx *evalCtx, agentName string, agent *ast.Agent, response string, depth int) (string, error) {
	expr, ok := agentHookExpr(agent, "after")
	if !ok {
		return response, nil
	}
	closure, err := evalHookClosure(ctx, agentName, "after", expr, depth)
	if err != nil {
		return "", err
	}
	result, err := invokeClosureWithEnv(closure, Env{"result": response}, depth)
	if err != nil {
		return "", fmt.Errorf("%s.after: %w", agentName, err)
	}
	if result == nil {
		return response, nil
	}
	s, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("%s.after must return a string (or nothing), got %s", agentName, typeName(result))
	}
	return s, nil
}
