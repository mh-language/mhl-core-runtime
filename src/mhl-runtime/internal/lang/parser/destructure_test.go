package parser

import "testing"

func TestDestructureExpandsToPlainStatements(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    var total = 0
    step S {
        var {data, revisao: note} = load()
        self.{total} = load()
        if (true) self.{total} = load()
    }
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body[1].Step.Body
	// var tmp + 2 vars, var tmp + 1 self assign, if
	if len(body) != 6 { // a brace-less if body expands in place, too
		t.Fatalf("want 6 statements after expansion, got %d", len(body))
	}
	for _, s := range body {
		if s.Destructure != nil {
			t.Fatal("a destructuring statement survived expansion")
		}
	}
	if body[0].Var == nil || body[0].Var.Name != "$destructure1" {
		t.Fatalf("first statement should bind the temporary, got %+v", body[0])
	}
	if body[1].Var.Name != "data" || body[2].Var.Name != "note" {
		t.Fatalf("var targets = %q, %q", body[1].Var.Name, body[2].Var.Name)
	}
	if field := body[2].Var.Value.Or.Head.Head.Head.Head.Head.Head.Operand.Ops[0].Member; field != "revisao" {
		t.Fatalf("renamed entry reads field %q, want revisao", field)
	}
	a := body[4].Assign
	if a == nil || a.Target.Primary.Ident != "self" || a.Target.Ops[0].Member != "total" {
		t.Fatalf("self.{...} should assign self.total, got %+v", body[4])
	}
	then := body[5].If.Then
	if len(then) != 2 || then[1].Assign == nil || then[1].Assign.Target.Ops[0].Member != "total" {
		t.Fatalf("a single-statement if body should expand in place, got %+v", then)
	}
}

// `base.{f} = expr` assigns each field to the base fragment's state:
// `base.f`, as `self.{f}` assigns `self.f`.
func TestDestructureToBase(t *testing.T) {
	prog, err := Parse(`
partial workflow W {
    var a = 0
    var b = 0
    entry step S {
        base.{a, x: b} = {a: 1, x: 2}
    }
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body[2].Step.Body
	if len(body) != 3 {
		t.Fatalf("want temporary + 2 assignments, got %d", len(body))
	}
	for i, want := range []string{"a", "b"} {
		a := body[i+1].Assign
		if a == nil || a.Target.Primary.Ident != "base" || a.Target.Ops[0].Member != want {
			t.Fatalf("entry %d should assign base.%s, got %+v", i, want, body[i+1])
		}
	}
}
