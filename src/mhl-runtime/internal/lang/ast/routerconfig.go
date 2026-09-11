package ast

import "fmt"

// This file holds the AST-literal-reading parts of a router declaration's
// config properties, mirroring agentconfig.go's split: these functions only
// ever read literals already present in the parse tree, shared by
// internal/lang/lint and internal/engine/interpreter.

// RouterAgentRefs reads a router's `agents: [...]` property — a bare array
// of identifiers, each naming an already-declared `agent`. Unlike
// AgentFallbackRefs, there is no inline `agent { ... }` literal form: a
// router only ever references agents declared elsewhere.
func RouterAgentRefs(router *Router) ([]string, error) {
	for _, prop := range router.Props {
		if prop.Name != "agents" {
			continue
		}
		arr := BareArray(prop.Value)
		if arr == nil {
			return nil, fmt.Errorf("router %q agents must be an array", router.Name)
		}
		names := make([]string, 0, len(arr.Items))
		for _, item := range arr.Items {
			name, ok := IdentValue(item)
			if !ok {
				return nil, fmt.Errorf("router %q agents entries must be declared agent names", router.Name)
			}
			names = append(names, name)
		}
		return names, nil
	}
	return nil, fmt.Errorf("router %q has no agents", router.Name)
}

// DeciderRef is a router's `decider: ...` property: either an inline
// `agent { ... }` literal or a bare name referring to another declared
// agent — the same two shapes FallbackRef already reads for an agent's own
// `fallback: [...]`. Resolving Name against the program (including
// import-alias handling) stays in each caller's own package, exactly like
// FallbackRef's doc comment already notes for that same pre-existing
// duplication between internal/lang/lint and internal/engine/interpreter.
type DeciderRef struct {
	Inline *Agent
	Name   string
}

// RouterDeciderRef reads a router's `decider: ...` property, if declared.
// The decider is the agent the LLM cascade calls to pick one of `agents:`
// — an ordinary, independently useful agent (it may even be a lightweight
// one dedicated to routing), not one of RouterBodyProperties'
// engine/command/... duplicated directly on the router. ok is false when
// the router declares no `decider` at all.
func RouterDeciderRef(router *Router) (ref DeciderRef, ok bool, err error) {
	for _, prop := range router.Props {
		if prop.Name != "decider" {
			continue
		}
		if inline, isInline := AgentValue(prop.Value); isInline {
			return DeciderRef{Inline: inline}, true, nil
		}
		if name, isName := IdentValue(prop.Value); isName {
			return DeciderRef{Name: name}, true, nil
		}
		return DeciderRef{}, true, fmt.Errorf("router %q decider must be an inline agent {...} block or a declared agent name", router.Name)
	}
	return DeciderRef{}, false, nil
}

// RouterHasDecider reports whether router declares a `decider` property at
// all (regardless of whether it resolves to a real agent — that's
// RouterDeciderRef's job). The LLM cascade is entirely optional: a router
// that declares no `decider` is a purely deterministic one — its `select`
// hook is the only way `.delegate()` can ever resolve an agent, and a
// `select` miss is a clear, dedicated error instead of an attempt to run an
// unconfigured decision call.
func RouterHasDecider(router *Router) bool {
	_, ok, _ := RouterDeciderRef(router)
	return ok
}

// RouterSelectExpr reads a router's `select` property, if declared — an
// ordinary Property whose Value is expected to resolve (at runtime, like any
// other expression) to a one-parameter lambda literal (`(prompt) -> {
// ... }`). This only reports whether the property is present at all; it's
// the shared building block for both the interpreter's evaluation of it and
// lint's static "can this router ever decide anything" check
// (checkRouterDecidable).
func RouterSelectExpr(router *Router) (*Expr, bool) {
	for _, p := range router.Props {
		if p.Name == "select" {
			return p.Value, true
		}
	}
	return nil, false
}

// RouterBodyProperty is one property allowed directly in a `router { ... }`
// body. RouterBodyProperties below is the single source of truth
// internal/lang/lint (checkRouterProperties) and internal/lsp (property
// completion) both derive their allow-list from, so adding one is a single
// edit here plus wiring its value into the interpreter's router.go.
type RouterBodyProperty struct {
	Name string
	// Doc is the one-line detail shown in editor completion.
	Doc string
}

// RouterBodyProperties is the allow-list of router body properties.
var RouterBodyProperties = []RouterBodyProperty{
	{Name: "agents", Doc: "[Agent, ...] — required; the declared agents .delegate() may choose among"},
	{Name: "select", Doc: "(prompt) -> agentName|null — deterministic hook tried before the LLM decision; an absent/null return cascades to the LLM if a decider is configured, else errors. A non-null return that names no declared agent is always an error (likely a typo), never a silent cascade — return nameof(Billing) instead of a string literal to catch that typo at mhl lint time"},
	{Name: "decider", Doc: "Agent | agent {...} — optional; an ordinary declared agent (or inline literal) the LLM cascade calls to pick one of agents when select is absent/inconclusive. Its own engine/command/args/endpoint/temperature/log/trace/retry/cache configure that call — nothing is duplicated on the router. Omit entirely for a purely deterministic router driven only by select"},
}

// RouterBodyPropertyNames returns the allow-list as a set, for a membership
// check.
func RouterBodyPropertyNames() map[string]bool {
	m := make(map[string]bool, len(RouterBodyProperties))
	for _, p := range RouterBodyProperties {
		m[p.Name] = true
	}
	return m
}
