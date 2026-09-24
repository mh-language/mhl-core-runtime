package parser

import "testing"

func TestPipelineSignatureParses(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		param     string
		paramType string
		returns   string
		max       string
	}{
		{"untyped", `workflow W { step S { log("x") } }`, "", "", "", ""},
		{"param only", `workflow W(req: In) { step S { log("x") } }`, "req", "In", "", ""},
		{"returns only", `pipeline W: Out { step S { log("x") } }`, "", "", "Out", ""},
		{"both", `workflow W(req: In): Out { step S { log("x") } }`, "req", "In", "Out", ""},
		{"inline shapes", `workflow W(req: { diff: string, base?: string }): { ok: bool } { step S { log("x") } }`, "req", "{ diff: string, base?: string }", "{ ok: bool }", ""},
		{"loop with max", `loop workflow W(req: In): Out max 3 { step S { log("x") } }`, "req", "In", "Out", "3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog, err := Parse(c.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			p := prog.Decls[0].Pipeline
			var param, paramType string
			if p.Param != nil {
				param, paramType = p.Param.Name, p.Param.Type.String()
			}
			if param != c.param || paramType != c.paramType || p.Returns.String() != c.returns || p.Max != c.max {
				t.Fatalf("got param=%q type=%q returns=%q max=%q", param, paramType, p.Returns.String(), p.Max)
			}
		})
	}
}

func TestBareQuestionMarkOnlyInShapeField(t *testing.T) {
	if _, err := Parse(`pipeline P { step S { var x = 1 ? 2 } }`); err == nil {
		t.Fatal("a bare `?` outside an object-shape field must not parse")
	}
}
