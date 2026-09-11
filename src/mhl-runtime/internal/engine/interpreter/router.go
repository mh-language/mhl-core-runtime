package interpreter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func findRouter(prog *ast.Program, name string) (*ast.Router, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Router != nil && decl.Router.Name == name {
			return decl.Router, true
		}
	}
	return nil, false
}

// routerAgents resolves a router's `agents: [...]` property to the actual
// declared agents, in order — error `"router %q: agent %q is not declared"`
// on a name that doesn't resolve, mirroring agentFallback's own check.
func routerAgents(prog *ast.Program, router *ast.Router) ([]*ast.Agent, error) {
	names, err := ast.RouterAgentRefs(router)
	if err != nil {
		return nil, err
	}
	agents := make([]*ast.Agent, 0, len(names))
	for _, name := range names {
		agent, ok := findAgent(prog, name)
		if !ok {
			return nil, fmt.Errorf("router %q: agent %q is not declared", router.Name, name)
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func findAgentByName(agents []*ast.Agent, name string) *ast.Agent {
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	return nil
}

func agentNames(agents []*ast.Agent) []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return names
}

// runRouterDelegate executes router's `.delegate(...)` call. It resolves the
// agent to run via a hybrid decision: a deterministic `select` hook runs
// first (routerSelect), if declared; only when it's absent or inconclusive
// does this fall back to an LLM decision call against router's `decider`
// agent (routerDecide) — and that cascade only happens at all when one is
// actually declared (ast.RouterHasDecider). A router with no `decider` is a
// deliberately, purely deterministic one: a `select` miss there is a clear,
// dedicated error rather than an attempt to run an unconfigured agent. The
// chosen agent then runs with call's original arguments — so it sees the
// same `prompt:`/`schema:` the caller passed to `.delegate(...)` — and its
// response is returned unchanged, gaining that agent's own
// before/after/retry/cache/fallback behavior for free.
func runRouterDelegate(ctx *evalCtx, routerName string, router *ast.Router, call *ast.Call, depth int) (string, error) {
	agents, err := routerAgents(ctx.prog, router)
	if err != nil {
		return "", err
	}
	if len(agents) == 0 {
		return "", fmt.Errorf("router %q: agents must declare at least one agent", routerName)
	}

	promptText, ok, err := resolvePromptArgument(ctx, call, depth)
	if err != nil {
		return "", fmt.Errorf("%s.delegate: %w", routerName, err)
	}
	if !ok || promptText == "" {
		return "", fmt.Errorf("%s.delegate requires a non-empty prompt", routerName)
	}

	selected, err := routerSelect(ctx, routerName, router, promptText, depth)
	if err != nil {
		return "", err
	}
	chosen := findAgentByName(agents, selected)
	if chosen == nil {
		// A non-empty selected that still didn't resolve is almost always a
		// typo (or a stale rename) in select's returned name, not "select
		// declined to decide" — treated the same way, it would silently
		// fall through to the LLM cascade (or the generic "not configured"
		// error below), masking the real bug. Surfacing it immediately, by
		// name, against the actual declared agents, is what lets a typo be
		// caught the first time this router is ever called instead of
		// misrouting or erroring confusingly downstream.
		if selected != "" {
			return "", fmt.Errorf("%s.select returned %q, which is not one of the declared agents (%s) — check for a typo, or use nameof(...) instead of a string literal so this is caught by mhl lint", routerName, selected, strings.Join(agentNames(agents), ", "))
		}
		if !ast.RouterHasDecider(router) {
			return "", fmt.Errorf("%s.delegate: select did not resolve an agent, and no decider is configured — declare a decider on the router to enable an LLM cascade, or make select exhaustive", routerName)
		}
		_, chosen, err = routerDecide(ctx, routerName, router, agents, promptText)
		if err != nil {
			return "", err
		}
	}

	return runAgent(ctx, chosen.Name, chosen, call, depth)
}

// routerSelect evaluates router's optional `select: (prompt) -> {...}` hook
// against promptText — reading the property via the shared ast.RouterSelectExpr
// (the same shape agent's before/after hooks read, agent_hooks.go's
// agentHookExpr, except select takes exactly one parameter instead of zero).
// An empty return (with a nil error) means select is either undeclared, or
// declared but returned nothing / a name that isn't one of router's declared
// agents — either way, the caller cascades to the LLM decision phase (if one
// is configured; see ast.RouterHasDecider).
func routerSelect(ctx *evalCtx, routerName string, router *ast.Router, promptText string, depth int) (string, error) {
	expr, ok := ast.RouterSelectExpr(router)
	if !ok {
		return "", nil
	}
	v, err := evalExprAt(ctx, expr, depth)
	if err != nil {
		return "", fmt.Errorf("%s.select: %w", routerName, err)
	}
	closure, ok := v.(*Closure)
	if !ok {
		return "", fmt.Errorf("%s.select must be a lambda, e.g. (prompt) -> { ... }", routerName)
	}
	if len(closure.def.Params) != 1 {
		return "", fmt.Errorf("%s.select must take exactly one parameter, e.g. (prompt) -> { ... }", routerName)
	}
	result, err := invokeClosureWithValues(closure, []any{promptText}, depth)
	if err != nil {
		return "", fmt.Errorf("%s.select: %w", routerName, err)
	}
	if result == nil {
		return "", nil
	}
	name, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("%s.select must return a string agent name (or nothing), got %s", routerName, typeName(result))
	}
	return name, nil
}

// routerDecider resolves router's `decider: ...` property to the actual
// agent to call — either the inline `agent { ... }` literal, or the
// declared agent ref.Name names, mirroring agentFallback's identical
// Inline-vs-Name resolution for `fallback: [...]`. deciderName is a display
// name for error messages and tracing: ref.Name when there is one, else a
// synthesized "<router>.decider" for an unnamed inline literal.
func routerDecider(prog *ast.Program, routerName string, router *ast.Router) (deciderName string, decider *ast.Agent, err error) {
	ref, ok, err := ast.RouterDeciderRef(router)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("router %q has no decider", routerName)
	}
	if ref.Inline != nil {
		return routerName + ".decider", ref.Inline, nil
	}
	agent, found := findAgent(prog, ref.Name)
	if !found {
		return "", nil, fmt.Errorf("router %q: decider agent %q is not declared", routerName, ref.Name)
	}
	return ref.Name, agent, nil
}

// routerDecide runs the LLM cascade: it resolves router's `decider` agent
// (routerDecider) and calls it — via the same runAgentAttempt agent.go's own
// `.run()` uses, so the decider's full engine/command/args/endpoint/
// temperature/retry/cache config applies exactly as it would for any other
// agent, with nothing duplicated on the router — with a "pick one of these
// agents" prompt built from agents' names (plus each agent's own optional
// `description` property, if declared) and promptText, the same prompt
// already passed to `.delegate(...)`.
func routerDecide(ctx *evalCtx, routerName string, router *ast.Router, agents []*ast.Agent, promptText string) (string, *ast.Agent, error) {
	deciderName, decider, err := routerDecider(ctx.prog, routerName, router)
	if err != nil {
		return "", nil, err
	}

	names := agentNames(agents)
	response, err := runAgentAttempt(ctx, deciderName, decider, routerDecisionPrompt(agents, promptText), routerDecisionSchema(names))
	if err != nil {
		return "", nil, fmt.Errorf("%s.delegate: %w", routerName, err)
	}

	chosenName := matchAgentName(response, names)
	if chosenName == "" {
		return "", nil, fmt.Errorf("%s.delegate: could not resolve a delegate for the given prompt (model answered %q)", routerName, strings.TrimSpace(response))
	}
	return chosenName, findAgentByName(agents, chosenName), nil
}

// routerDecisionPrompt builds the "pick one of these agents" prompt sent to
// the decider agent during the cascade phase. Each agent contributes its bare
// name, plus its optional `description` property (ast.AgentDescription) when
// declared — giving the decider more to go on than a name alone, with no
// separate per-agent description needed on the router itself.
func routerDecisionPrompt(agents []*ast.Agent, promptText string) string {
	var b strings.Builder
	b.WriteString("You are a routing decision engine. Choose exactly one of the following agent names to handle the request below. Respond with only the chosen agent name and nothing else.\n\nAgents:\n")
	for _, a := range agents {
		b.WriteString("- ")
		b.WriteString(a.Name)
		if desc, ok := ast.AgentDescription(a); ok && desc != "" {
			b.WriteString(": ")
			b.WriteString(desc)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nRequest:\n")
	b.WriteString(promptText)
	return b.String()
}

// routerDecisionSchema builds a JSON Schema constraining the decision
// call's answer to one of names — honored by the ollama/* engine (see
// adapters.Ollama.Run's format field); a cli/* engine only sees it if its
// own args: declares a literal "${schema}" placeholder.
func routerDecisionSchema(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return fmt.Sprintf(`{"type":"string","enum":[%s]}`, strings.Join(quoted, ","))
}

// matchAgentName matches an LLM decision response against the declared
// agent names — first an exact match (trimmed of surrounding whitespace and
// quotes, case-insensitive), then, for a model that answered with more than
// just the bare name, whichever declared name occurs earliest in the
// response (ties broken by the longer name at that position) — the "answer
// first, explanation after" shape a model typically produces, and the one
// that stays correct even when the response also quotes back the full list
// of candidate names (e.g. as part of restating the question). Returns ""
// when no declared name occurs in the response at all.
func matchAgentName(response string, names []string) string {
	resp := strings.Trim(strings.TrimSpace(response), `"'`)
	for _, n := range names {
		if strings.EqualFold(resp, n) {
			return n
		}
	}
	lowerResp := strings.ToLower(resp)
	bestIndex := -1
	best := ""
	for _, n := range names {
		idx := strings.Index(lowerResp, strings.ToLower(n))
		if idx < 0 {
			continue
		}
		if bestIndex == -1 || idx < bestIndex || (idx == bestIndex && len(n) > len(best)) {
			bestIndex = idx
			best = n
		}
	}
	return best
}
