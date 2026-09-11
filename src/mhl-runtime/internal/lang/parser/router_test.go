package parser

import "testing"

func TestRouterDeclarationParses(t *testing.T) {
	prog, err := Parse(`
agent Billing { command: "claude" }
agent Support { command: "claude" }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt == "invoice") { return "Billing" }
        return null
    }
    engine: "cli/claude"
    command: "claude"
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prog.Decls) != 3 {
		t.Fatalf("expected 3 declarations, got %d", len(prog.Decls))
	}
	router := prog.Decls[2].Router
	if router == nil || router.Name != "Frontdesk" {
		t.Fatalf("expected a Router declaration named Frontdesk, got %+v", prog.Decls[2])
	}
	names := make([]string, 0, len(router.Props))
	for _, p := range router.Props {
		names = append(names, p.Name)
	}
	want := []string{"agents", "select", "engine", "command"}
	if len(names) != len(want) {
		t.Fatalf("expected properties %v, got %v", want, names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("expected property %d to be %q, got %q", i, n, names[i])
		}
	}
}

func TestRouterRequiresName(t *testing.T) {
	if _, err := Parse(`router { agents: [Billing] }`); err == nil {
		t.Fatalf("expected a parse error for a nameless router")
	}
}

func TestRouterDelegateCallParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    step S {
        var reply = Frontdesk.delegate(prompt: "hi")
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	stmt := prog.Decls[0].Pipeline.Body[0].Step.Body[0]
	if stmt.Var == nil || stmt.Var.Name != "reply" {
		t.Fatalf("expected a var declaration named reply, got %+v", stmt)
	}
}
