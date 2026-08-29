package parser

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func TestEnumDeclParses(t *testing.T) {
	for _, src := range []string{
		`enum Status { Draft, Published, Archived }`,
		"enum Status {\n  Draft\n  Published\n}",
		`enum Status { Draft, Published, }`,
		`enum Empty { }`,
	} {
		prog, err := Parse(src + "\npipeline P { step S { log(\"x\") } }")
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if prog.Decls[0].Enum == nil {
			t.Fatalf("%q: no enum decl", src)
		}
	}
	prog, _ := Parse(`enum Status { Draft, Published, Archived }` + "\npipeline P { step S { log(\"x\") } }")
	if got := prog.Decls[0].Enum.Variants; len(got) != 3 || got[0] != "Draft" || got[2] != "Archived" {
		t.Fatalf("variants = %v", got)
	}
}

func TestMatchExprParses(t *testing.T) {
	prog, err := Parse(`
enum Status { Draft, Live }
pipeline P {
    step S {
        var a = match status { Status.Draft -> "d" Status.Live -> "l" _ -> "?" }
        var b = f(match n { 1 -> "one" _ -> "many" })
        var match = 3
    }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stmts := prog.Decls[1].Pipeline.Body[0].Step.Body
	pf := ast.BarePostfix(stmts[0].Var.Value)
	if pf == nil || pf.Primary == nil || pf.Primary.Match == nil {
		t.Fatalf("first statement value is not a bare match expr")
	}
	m := pf.Primary.Match
	if len(m.Arms) != 3 || !m.Arms[2].Wildcard {
		t.Fatalf("arms = %+v", m.Arms)
	}
	// `var match = 3` — `match` still usable as an identifier.
	if stmts[2].Var == nil || stmts[2].Var.Name != "match" {
		t.Fatalf("expected `var match = 3` to parse, got %+v", stmts[2])
	}
}
