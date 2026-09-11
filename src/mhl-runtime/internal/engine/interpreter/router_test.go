package interpreter

import (
	"io"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// parseDelegateCall parses src as a standalone expression shaped like
// `Router.delegate(prompt: "...")` and returns the trailing Call node, the
// same way runRouterDelegate's caller (evalPostfix) would hand it one.
func parseDelegateCall(t *testing.T, src string) *ast.Call {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", src, err)
	}
	pf := ast.BarePostfix(expr)
	if pf == nil || len(pf.Ops) == 0 {
		t.Fatalf("ParseExpr(%q) did not produce a call", src)
	}
	call := pf.Ops[len(pf.Ops)-1].Call
	if call == nil {
		t.Fatalf("ParseExpr(%q) did not end in a call", src)
	}
	return call
}

// TestFindRouterFindsDeclaredRouter proves findRouter resolves a declared
// `router` by name, mirroring findAgent.
func TestFindRouterFindsDeclaredRouter(t *testing.T) {
	prog, err := parser.Parse(`
agent Billing { command: "echo" args: ["billing"] }
router Frontdesk { agents: [Billing] }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := findRouter(prog, "Frontdesk"); !ok {
		t.Fatal("router Frontdesk not found")
	}
	if _, ok := findRouter(prog, "Nope"); ok {
		t.Fatal("findRouter unexpectedly found an undeclared router")
	}
}

// TestRunRouterDelegateUsesSelectHookWithoutLLMCall proves that when
// `select` deterministically resolves an agent, delegate never falls back to
// the LLM decision phase — the router's decider here would fail loudly (an
// unknown executable) if the cascade ever ran, so a passing test proves it
// didn't.
func TestRunRouterDelegateUsesSelectHookWithoutLLMCall(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Support { command: "echo" args: ["support-said:"] }
export agent BrokenDecider { command: "this-command-does-not-exist-and-must-never-run" }

export router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt == "invoice question") { return "Billing" }
        return null
    }
    decider: BrokenDecider
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "invoice question")`)

	got, err := runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err != nil {
		t.Fatalf("runRouterDelegate: %v", err)
	}
	want := "billing-said: invoice question"
	if got != want {
		t.Fatalf("runRouterDelegate = %q, want %q", got, want)
	}
}

// TestRunRouterDelegateCascadesToLLMWhenSelectIsInconclusive proves that an
// absent `select` result falls back to router's `decider` agent to decide,
// and that the chosen agent is then actually run.
func TestRunRouterDelegateCascadesToLLMWhenSelectIsInconclusive(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Support { command: "echo" args: ["support-said:"] }
export agent Decider { command: "sh" args: ["-c", "printf 'Support'"] }

export router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> null
    decider: Decider
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "my account is locked")`)

	got, err := runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err != nil {
		t.Fatalf("runRouterDelegate: %v", err)
	}
	want := "support-said: my account is locked"
	if got != want {
		t.Fatalf("runRouterDelegate = %q, want %q", got, want)
	}
}

// TestRunRouterDelegateWithNoSelectCascadesToLLM proves the cascade also
// fires when `select` is declared at all, not just when it returns null.
func TestRunRouterDelegateWithNoSelectCascadesToLLM(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Decider { command: "sh" args: ["-c", "printf 'Billing'"] }

export router Frontdesk {
    agents: [Billing]
    decider: Decider
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	got, err := runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err != nil {
		t.Fatalf("runRouterDelegate: %v", err)
	}
	want := "billing-said: anything"
	if got != want {
		t.Fatalf("runRouterDelegate = %q, want %q", got, want)
	}
}

// TestRunRouterDelegateErrorsOnUndeclaredAgentRef proves an `agents: [...]`
// entry naming an undeclared agent fails before any decision phase runs.
func TestRunRouterDelegateErrorsOnUndeclaredAgentRef(t *testing.T) {
	src := `
export router Frontdesk {
    agents: [Nope]
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	_, err = runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err == nil {
		t.Fatal("expected an error for a router referencing an undeclared agent")
	}
}

// TestRunRouterDelegateWorksWithoutAnyDecisionConfig proves the LLM cascade
// is entirely optional: a router that declares only `select` (no `decider`
// at all) still resolves and runs correctly as long as `select` is
// exhaustive for the prompts it receives.
func TestRunRouterDelegateWorksWithoutAnyDecisionConfig(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Support { command: "echo" args: ["support-said:"] }

export router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return "Billing" }
        return "Support"
    }
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "my account is locked")`)

	got, err := runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err != nil {
		t.Fatalf("runRouterDelegate: %v", err)
	}
	want := "support-said: my account is locked"
	if got != want {
		t.Fatalf("runRouterDelegate = %q, want %q", got, want)
	}
}

// TestRunRouterDelegateErrorsClearlyWhenSelectMissesAndNoDecisionConfig
// proves that a purely deterministic router (no `decider` at all) whose
// `select` fails to resolve an agent fails with a clear, dedicated error —
// not an attempt to run an unconfigured decider.
func TestRunRouterDelegateErrorsClearlyWhenSelectMissesAndNoDecisionConfig(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }

export router Frontdesk {
    agents: [Billing]
    select: (prompt) -> null
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	_, err = runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err == nil {
		t.Fatal("expected an error when select misses and no decider is configured")
	}
	if !strings.Contains(err.Error(), "no decider is configured") {
		t.Fatalf("expected a clear no-decider error, got: %v", err)
	}
}

// TestRunRouterDelegateWithNoSelectPropertyErrorsClearlyWithoutDecisionConfig
// covers the same optional-cascade guarantee when `select` is absent
// entirely, not just inconclusive.
func TestRunRouterDelegateWithNoSelectPropertyErrorsClearlyWithoutDecisionConfig(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }

export router Frontdesk {
    agents: [Billing]
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	_, err = runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err == nil {
		t.Fatal("expected an error when neither select nor a decider is configured")
	}
	if !strings.Contains(err.Error(), "no decider is configured") {
		t.Fatalf("expected a clear no-decider error, got: %v", err)
	}
}

// TestRunRouterDelegateErrorsClearlyWhenSelectReturnsATypoEvenWithDecisionConfig
// proves that a select hook returning a non-empty name that doesn't match
// any declared agent is treated as a bug (a typo, a stale rename) — not as
// "select declined to decide" — and fails immediately with a message naming
// the bad value and the real agent names, even when a decision engine IS
// configured and could otherwise have masked it by silently taking over.
func TestRunRouterDelegateErrorsClearlyWhenSelectReturnsATypoEvenWithDecisionConfig(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Support { command: "echo" args: ["support-said:"] }
export agent BrokenDecider { command: "this-command-does-not-exist-and-must-never-run" }

export router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> "Biling"
    decider: BrokenDecider
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	_, err = runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err == nil {
		t.Fatal("expected an error for a select return that names no declared agent")
	}
	if !strings.Contains(err.Error(), `select returned "Biling"`) || !strings.Contains(err.Error(), "Billing, Support") {
		t.Fatalf("expected a typo-shaped error naming the bad value and the real agents, got: %v", err)
	}
}

// TestRunRouterDelegateErrorsClearlyWhenSelectReturnsATypoWithoutDecisionConfig
// covers the same case for a purely deterministic router (no decider at
// all): the error must still name the mismatch, not the generic "no
// decider is configured" message.
func TestRunRouterDelegateErrorsClearlyWhenSelectReturnsATypoWithoutDecisionConfig(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }

export router Frontdesk {
    agents: [Billing]
    select: (prompt) -> "Biling"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	_, err = runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err == nil {
		t.Fatal("expected an error for a select return that names no declared agent")
	}
	if !strings.Contains(err.Error(), `select returned "Biling"`) {
		t.Fatalf("expected a typo-shaped error, got: %v", err)
	}
	if strings.Contains(err.Error(), "no decider is configured") {
		t.Fatalf("expected the typo-specific error, not the generic no-decider one, got: %v", err)
	}
}

// TestRouterDecisionPromptIncludesAgentDescriptions proves that an agent's
// optional `description` property is folded into the decision prompt right
// after its name, and that an agent declaring no description still gets a
// bare name line (no dangling ": ").
func TestRouterDecisionPromptIncludesAgentDescriptions(t *testing.T) {
	prog, err := parser.Parse(`
agent Billing { command: "echo" description: "Handles invoices and refunds" }
agent Support { command: "echo" }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	billing, ok := findAgent(prog, "Billing")
	if !ok {
		t.Fatal("agent Billing not found")
	}
	support, ok := findAgent(prog, "Support")
	if !ok {
		t.Fatal("agent Support not found")
	}

	got := routerDecisionPrompt([]*ast.Agent{billing, support}, "my invoice is wrong")
	if !strings.Contains(got, "- Billing: Handles invoices and refunds\n") {
		t.Fatalf("expected Billing's description in the prompt, got: %s", got)
	}
	if !strings.Contains(got, "- Support\n") {
		t.Fatalf("expected Support's bare name in the prompt, got: %s", got)
	}
}

// TestRunRouterDelegateErrorsWhenDecisionMatchesNoAgent proves that when the
// LLM cascade's answer matches none of the declared agent names, delegate
// fails with a clear error instead of silently picking one.
func TestRunRouterDelegateErrorsWhenDecisionMatchesNoAgent(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Decider { command: "sh" args: ["-c", "printf 'Nobody'"] }

export router Frontdesk {
    agents: [Billing]
    decider: Decider
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	router, ok := findRouter(prog, "Frontdesk")
	if !ok {
		t.Fatal("router Frontdesk not found")
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "anything")`)

	_, err = runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err == nil {
		t.Fatal("expected an error when the decision call matches no declared agent")
	}
}
