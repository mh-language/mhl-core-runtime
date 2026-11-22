package parser

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func TestGateDocs(t *testing.T) {
	prog, err := Parse(`workflow W {
    var ok = false
    step S {
        /// d: the reviewer's call
        var d = pause("a", decision: bool)
        /// set on the workflow
        self.ok = pause("b", decision: bool)
        // a plain comment is no doc
        const c = pause("c", decision: bool)
        log(pause("d", decision: bool))
        var p = pause("plain")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	var got []ast.GateDoc
	var plain int
	for _, st := range prog.Decls[0].Pipeline.Body[1].Step.Body {
		var e *ast.Expr
		switch {
		case st.Var != nil:
			e = st.Var.Value
		case st.Const != nil:
			e = st.Const.Value
		case st.Assign != nil:
			e = st.Assign.Value
		case st.Expr != nil:
			e = st.Expr.Expr
		}
		call := ast.BarePostfix(e).Ops[0].Call
		if call.Gate == nil {
			plain++
			continue
		}
		got = append(got, *call.Gate)
	}
	want := []ast.GateDoc{{Name: "d", Doc: "the reviewer's call"}, {Name: "ok", Doc: "set on the workflow"}, {Name: "c"}}
	if len(got) != len(want) || plain != 2 {
		t.Fatalf("gates = %+v, plain = %d", got, plain)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gate %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
