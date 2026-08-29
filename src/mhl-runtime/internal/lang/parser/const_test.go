package parser

import "testing"

func TestConstDeclParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    const LIMIT = 3
    step S {
        const name = "x"
        var const = 5
    }
}
tool T { f() -> { const k = 1
return k } }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body
	if body[0].Const == nil || body[0].Const.Name != "LIMIT" {
		t.Fatalf("pipeline-level const not parsed: %+v", body[0])
	}
	stmts := body[1].Step.Body
	if stmts[0].Const == nil || stmts[0].Const.Name != "name" {
		t.Fatalf("step-level const not parsed: %+v", stmts[0])
	}
	// `const` stays a soft keyword — `var const = 5` still parses.
	if stmts[1].Var == nil || stmts[1].Var.Name != "const" {
		t.Fatalf("`var const = 5` should parse, got %+v", stmts[1])
	}
}
