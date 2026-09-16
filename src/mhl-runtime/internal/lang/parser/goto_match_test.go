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
