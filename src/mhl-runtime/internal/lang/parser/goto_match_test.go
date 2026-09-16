package parser

import "testing"

func TestGotoMatchParses(t *testing.T) {
	prog, err := Parse(`workflow W {
  step Gate {
    goto match artifact {
      "brief" -> Brief
      "adr" -> Adr
      _ -> fail("unknown: " + artifact)
    }
  }
  step Brief {}
  step Adr {}
}`)
	if err != nil {
		t.Fatal(err)
	}
	m := prog.Decls[0].Pipeline.Body[0].Step.Body[0].GotoMatch
	if m == nil || len(m.Arms) != 3 || m.Arms[0].Target != "Brief" || m.Arms[2].Fail == nil {
		t.Fatalf("unexpected goto match: %+v", m)
	}
}

func TestWorkflowRouteParses(t *testing.T) {
	prog, err := Parse(`workflow W {
  route Generate(artifact: string) { "brief" -> Brief _ -> fail("unknown") }
  step Gate { goto Generate(artifact) }
  step Brief {}
}`)
	if err != nil {
		t.Fatal(err)
	}
	p := prog.Decls[0].Pipeline
	if p.Body[0].Route == nil || p.Body[0].Route.Name != "Generate" || len(p.Body[0].Route.Arms) != 2 {
		t.Fatalf("unexpected route: %+v", p.Body[0].Route)
	}
	g := p.Body[1].Step.Body[0].Goto
	if g == nil || !g.Called || g.Target != "Generate" || len(g.Args) != 1 {
		t.Fatalf("unexpected route call: %+v", g)
	}
}
