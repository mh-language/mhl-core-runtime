package parser

import "testing"

func TestWorkflowAliasParses(t *testing.T) {
	prog, err := Parse(`
workflow Flow { step A { log("x") } }
workflow Delivery = Flow with { level: Level.delivery, n: 1 } {
    description: "d"
}
pipeline Bare = Flow
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := prog.Decls[1].Alias
	if a == nil || a.Kind != "workflow" || a.Name != "Delivery" || a.Target != "Flow" || len(a.Bound.Fields) != 2 || a.Description() != "d" {
		t.Fatalf("alias = %+v", a)
	}
	b := prog.Decls[2].Alias
	if b == nil || b.Kind != "pipeline" || b.Bound != nil || len(b.Props) != 0 {
		t.Fatalf("bare alias = %+v", b)
	}
}
