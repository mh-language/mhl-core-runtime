package parser

import "testing"

func TestShorthandObjectFieldValue(t *testing.T) {
	e, err := ParseExpr(`{artifact, n: 1}`)
	if err != nil {
		t.Fatal(err)
	}
	fields := e.Or.Head.Head.Head.Head.Head.Head.Operand.Primary.Object.Fields
	if len(fields) != 2 {
		t.Fatalf("want 2 fields, got %d", len(fields))
	}
	f := fields[0]
	if f.Shorthand != nil || f.KeyIdent == nil || *f.KeyIdent != "artifact" || f.Value == nil {
		t.Fatalf("shorthand field not expanded: %+v", f)
	}
	if got := f.Value.Or.Head.Head.Head.Head.Head.Head.Operand.Primary.Ident; got != "artifact" {
		t.Fatalf("shorthand value = %q, want identifier artifact", got)
	}
}

func TestShorthandNamedArgumentValue(t *testing.T) {
	e, err := ParseExpr(`f(project_id:, x)`)
	if err != nil {
		t.Fatal(err)
	}
	args := e.Or.Head.Head.Head.Head.Head.Head.Operand.Ops[0].Call.Args
	if len(args) != 2 || args[0].Name != "project_id" || args[0].Value == nil {
		t.Fatalf("shorthand argument not expanded: %+v", args)
	}
	if got := args[0].Value.Or.Head.Head.Head.Head.Head.Head.Operand.Primary.Ident; got != "project_id" {
		t.Fatalf("argument value = %q, want identifier project_id", got)
	}
	if args[1].Name != "" || args[1].Value == nil {
		t.Fatalf("positional argument changed: %+v", args[1])
	}
	if e, err := ParseExpr(`f()`); err != nil || len(e.Or.Head.Head.Head.Head.Head.Head.Operand.Ops[0].Call.Args) != 0 {
		t.Fatalf("f() must still have no arguments (err %v)", err)
	}
}

// Keywords lex as identifiers, and an expression body is tried before a
// block body — a shorthand field must never swallow a block such as
// `{ return x }`.
func TestShorthandDoesNotSwallowBlocks(t *testing.T) {
	prog, err := Parse(`
tool T {
    a(x: any): any -> { return x }
    b(x: any): any -> {
        var y = x
        return y
    }
}
pipeline P {
    step S {
        var f = (x) -> { return x }
        if (true) { log("x") }
    }
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, m := range prog.Decls[0].Tool.Methods {
		if m.Block == nil {
			t.Fatalf("method %s parsed as an expression body (object), not a block", m.Name)
		}
	}
	if _, err := ParseExpr(`{return}`); err == nil {
		t.Fatal("a keyword must not be accepted as a shorthand field name")
	}
	if _, err := ParseExpr(`{"key"}`); err == nil {
		t.Fatal("a string key has no shorthand form")
	}
}
