package interpreter

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// toolRef and mcpServerRef are first-class runtime values binding to one
// declared `tool`/`mcp_server` — what a hook navigates to by name off its
// `tool`/`mcp` parameter (see hookRefValues). Unlike a bare declaration
// name (which findTool/findMCPServer resolve globally, against the whole
// program), a value of one of these types only exists inside the map the
// hook call binds `tool`/`mcp` to: applyTrailers' dispatch (eval.go) is
// what makes `tool.execution.read_file(...)`/`mcp.GitHub.call(...)` work
// off a map field holding one of these, reusing the exact same
// evalToolCall/evalMCPServerCall a declared name's own dispatch already
// calls into. This is real scoping, not the best-effort prompt-text kind
// agent_scope.go's `tools:`/`mcp_servers:` folding is limited to: a hook
// body can navigate to exactly the tools/mcp_servers the agent declared
// and nothing else — a name that was never a map key simply isn't
// reachable, the same way a tool method's fresh, parameters-only Env
// already keeps it from reaching a caller's local variables.
//
// allowedMethods narrows a toolRef further, to the method-level: nil means
// every method the tool declares is callable (the agent's `tools:` entry
// for it was a bare name, e.g. `execution`); non-nil restricts calls to
// exactly the method names present as keys (the entry was one or more
// dotted `tool.method` references, e.g. `execution.read_file`). A tool
// referenced by both a bare name and a dotted one anywhere in `tools:` is
// unrestricted — the bare form always wins, since a method-level
// restriction the same list also grants full access to would be
// meaningless.
type toolRef struct {
	tool           *ast.Tool
	allowedMethods map[string]bool
}
type mcpServerRef struct{ server *ast.MCPServer }

// hookRefValues resolves an agent's declared `tools:`/`mcp_servers:` scope
// (agentToolScope, agent_scope.go) into the two bound values a `before`/
// `after` hook receives: `tool`/`mcp` are each a map from declared name to
// a *toolRef/*mcpServerRef, one entry per distinct tool/mcp_server named in
// `tools:`/`mcp_servers:` — so `before: (mcp, tool) -> {
// tool.execution.read_file(...) }` navigates to whichever of the agent's
// declared tools it needs by name, not just a single implicit one. Either
// return is nil when the agent declares none of that kind, or when every
// declared name failed to resolve (already caught earlier by
// applyAgentDeclaredToolScope's validateDeclaredScope, so that shouldn't
// happen on a call that got this far).
func hookRefValues(prog *ast.Program, agent *ast.Agent) (mcpVal, toolVal any) {
	tools, mcpServers := agentToolScope(agent)
	if len(tools) > 0 {
		toolMap := make(map[string]any, len(tools))
		for toolName, allowedMethods := range groupToolRefsByName(tools) {
			if t, ok := findTool(prog, toolName); ok {
				toolMap[toolName] = &toolRef{tool: t, allowedMethods: allowedMethods}
			}
		}
		if len(toolMap) > 0 {
			toolVal = toolMap
		}
	}
	if len(mcpServers) > 0 {
		mcpMap := make(map[string]any, len(mcpServers))
		for _, name := range mcpServers {
			if s, ok := findMCPServer(prog, name); ok {
				mcpMap[name] = &mcpServerRef{server: s}
			}
		}
		if len(mcpMap) > 0 {
			mcpVal = mcpMap
		}
	}
	return mcpVal, toolVal
}

// groupToolRefsByName folds a `tools:` list — a mix of bare tool names
// (`execution`) and dotted tool.method references (`execution.read_file`)
// — into one allowedMethods set per distinct base tool name: nil for a
// name that appeared bare anywhere in the list (unrestricted), or the
// union of every method named for it otherwise.
func groupToolRefsByName(tools []string) map[string]map[string]bool {
	groups := make(map[string]map[string]bool, len(tools))
	for _, ref := range tools {
		toolName, method, hasMethod := strings.Cut(ref, ".")
		methods, seen := groups[toolName]
		if !hasMethod {
			groups[toolName] = nil // unrestricted, regardless of what was seen before
			continue
		}
		if seen && methods == nil {
			continue // already unrestricted by an earlier bare entry; stays that way
		}
		if methods == nil {
			methods = map[string]bool{}
		}
		methods[method] = true
		groups[toolName] = methods
	}
	return groups
}

// agentHookExpr reads an agent's `before`/`after` property, if declared —
// an ordinary Property whose Value is a lambda literal, the same shape
// `filter(items, (item) -> ...)`'s predicate argument already is; no new
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
		return nil, fmt.Errorf("%s.%s must be a lambda, e.g. (mcp, tool) -> { ... }", agentName, hookName)
	}
	return closure, nil
}

// runAgentBeforeHook evaluates an agent's `before: (mcp, tool) -> {...}`
// hook, if declared, and returns the object it returned — nil, nil when
// the agent declares no `before` at all. The hook is an ordinary closure
// (newClosure/evalPrimary's Lambda case), so it captures ctx's env at the
// point runAgent calls this — the calling step's own variables are visible
// to it, exactly like any other lambda created there.
//
// mhl agents are CLI subprocesses with no structural tool-calling channel
// of their own (agent_scope.go's doc comment). `before` is what lets a
// pipeline hand an agent real, already-fetched data instead of hoping the
// model reads and acts on a text instruction: its returned object's fields
// become named bindings runAgent makes available to the prompt's own
// "${...}" interpolation, so `before: (mcp, tool) -> { return {mcp_result:
// mcp.GitHub.call(...), tool_result: tool.execution.method(...)} }` really
// does make `${mcp_result}`/`${tool_result}` resolve inside `prompt: "..."`
// — with real, already-executed values, not a hint the model may or may
// not act on.
func runAgentBeforeHook(ctx *evalCtx, agentName string, agent *ast.Agent, depth int) (map[string]any, error) {
	expr, ok := agentHookExpr(agent, "before")
	if !ok {
		return nil, nil
	}
	closure, err := evalHookClosure(ctx, agentName, "before", expr, depth)
	if err != nil {
		return nil, err
	}
	mcpVal, toolVal := hookRefValues(ctx.prog, agent)
	result, err := invokeClosureWithValues(closure, []any{mcpVal, toolVal}, depth)
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

// runAgentAfterHook evaluates an agent's `after: (mcp, tool, result) ->
// {...}` hook, if declared, against response — response unchanged, nil
// when the agent declares no `after`. A non-nil string return replaces
// response as what `.run()` ultimately returns to the caller; any other
// return (including none) leaves response unchanged — `after` is for a
// side effect (log, persist, validate, chain another call) far more often
// than for rewriting the response, so passthrough is the default a hook
// doesn't have to opt into by re-returning its own `result` parameter.
func runAgentAfterHook(ctx *evalCtx, agentName string, agent *ast.Agent, response string, depth int) (string, error) {
	expr, ok := agentHookExpr(agent, "after")
	if !ok {
		return response, nil
	}
	closure, err := evalHookClosure(ctx, agentName, "after", expr, depth)
	if err != nil {
		return "", err
	}
	mcpVal, toolVal := hookRefValues(ctx.prog, agent)
	result, err := invokeClosureWithValues(closure, []any{mcpVal, toolVal, response}, depth)
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
