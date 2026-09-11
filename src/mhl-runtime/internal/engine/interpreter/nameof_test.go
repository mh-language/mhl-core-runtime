package interpreter

import (
	"io"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// TestNameofResolvesADeclaredAgentName proves nameof(Billing) returns
// "Billing" when Billing is a declared agent.
func TestNameofResolvesADeclaredAgentName(t *testing.T) {
	prog, err := parser.Parse(`
agent Billing { command: "echo" }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	expr, err := parser.ParseExpr(`nameof(Billing)`)
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	got, err := evalExprAt(ctx, expr, 0)
	if err != nil {
		t.Fatalf("evalExprAt: %v", err)
	}
	if got != "Billing" {
		t.Fatalf("nameof(Billing) = %v, want %q", got, "Billing")
	}
}

// TestNameofResolvesEveryDeclarationKind proves nameof isn't agent-only —
// it works for any top-level declared name.
func TestNameofResolvesEveryDeclarationKind(t *testing.T) {
	prog, err := parser.Parse(`
agent Billing { command: "echo" }
router Frontdesk { agents: [Billing] select: (prompt) -> nameof(Billing) }
memory Session { type: "kv" path: ".mhl/session.json" }
tool Text { slug(value: string): string -> value.to_lower() }
prompt Greeting() { "hi" }
pipeline Main { step S { var x = 1 } }
type UserId = string
enum Status { Draft, Published }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	for _, name := range []string{"Billing", "Frontdesk", "Session", "Text", "Greeting", "Main", "UserId", "Status"} {
		expr, err := parser.ParseExpr("nameof(" + name + ")")
		if err != nil {
			t.Fatalf("ParseExpr(%s): %v", name, err)
		}
		got, err := evalExprAt(ctx, expr, 0)
		if err != nil {
			t.Fatalf("nameof(%s): %v", name, err)
		}
		if got != name {
			t.Fatalf("nameof(%s) = %v, want %q", name, got, name)
		}
	}
}

// TestNameofErrorsOnUndeclaredName proves a typo in nameof's argument is a
// clear, immediate error rather than silently returning the literal text.
func TestNameofErrorsOnUndeclaredName(t *testing.T) {
	prog, err := parser.Parse(`agent Billing { command: "echo" }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	expr, err := parser.ParseExpr(`nameof(Biling)`)
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	_, err = evalExprAt(ctx, expr, 0)
	if err == nil {
		t.Fatal("expected an error for an undeclared name")
	}
	if got := err.Error(); got != `nameof: "Biling" is not a declared name` {
		t.Fatalf("unexpected error: %q", got)
	}
}

// TestNameofRejectsNonIdentifierArguments proves nameof requires a bare
// identifier — a string literal or any other expression is rejected, since
// the whole point is a real, lint/editor-checkable reference.
func TestNameofRejectsNonIdentifierArguments(t *testing.T) {
	prog, err := parser.Parse(`agent Billing { command: "echo" }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx := &evalCtx{prog: prog, env: Env{}, out: io.Discard}
	for _, src := range []string{`nameof("Billing")`, `nameof(1 + 1)`} {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("ParseExpr(%s): %v", src, err)
		}
		if _, err := evalExprAt(ctx, expr, 0); err == nil {
			t.Fatalf("expected %s to be rejected", src)
		}
	}
}

// TestRunRouterDelegateSelectUsingNameofAvoidsTypoRisk proves the intended
// end-to-end usage: a router's select hook returning nameof(Billing)
// instead of the string literal "Billing" resolves and runs correctly.
func TestRunRouterDelegateSelectUsingNameofAvoidsTypoRisk(t *testing.T) {
	src := `
export agent Billing { command: "echo" args: ["billing-said:"] }
export agent Support { command: "echo" args: ["support-said:"] }

export router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return nameof(Billing) }
        return nameof(Support)
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
	call := parseDelegateCall(t, `Frontdesk.delegate(prompt: "an invoice question")`)

	got, err := runRouterDelegate(ctx, "Frontdesk", router, call, 0)
	if err != nil {
		t.Fatalf("runRouterDelegate: %v", err)
	}
	want := "billing-said: an invoice question"
	if got != want {
		t.Fatalf("runRouterDelegate = %q, want %q", got, want)
	}
}
