package parser

import (
	"testing"
)

// TestTypeExprAcrossAllAnnotationSites confirms the new *ast.TypeExpr
// grammar (bare keyword, array suffix, nested array, inline object shape,
// and a mix of both) parses identically at all four annotation sites —
// ast.Param.Type, ast.PipelineInput.Type, ast.Field.Type, and
// ast.ToolMethod.Returns — since all four share the exact same TypeExpr
// grammar rule.
func TestTypeExprAcrossAllAnnotationSites(t *testing.T) {
	cases := []struct {
		name       string
		annotation string
	}{
		{"bare keyword", "string"},
		{"single array suffix", "string[]"},
		{"nested array", "string[][]"},
		{"inline object shape", "{ name: string, age: number }"},
		{"mixed shape+array", "{ tags: string[], meta: { active: bool } }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Param (tool method parameter).
			prog, err := Parse("tool T { m(v: " + c.annotation + ") -> v }")
			if err != nil {
				t.Fatalf("Param: parse: %v", err)
			}
			if got := stripSpaces(prog.Decls[0].Tool.Methods[0].Params[0].Type.String()); got != stripSpaces(c.annotation) {
				t.Errorf("Param.Type.String() = %q, want %q", got, stripSpaces(c.annotation))
			}

			// ToolMethod.Returns.
			prog, err = Parse("tool T { m(): " + c.annotation + " -> 1 }")
			if err != nil {
				t.Fatalf("Returns: parse: %v", err)
			}
			if got := stripSpaces(prog.Decls[0].Tool.Methods[0].Returns.String()); got != stripSpaces(c.annotation) {
				t.Errorf("Returns.String() = %q, want %q", got, stripSpaces(c.annotation))
			}

			// PipelineInput.
			prog, err = Parse("pipeline P { input v: " + c.annotation + "\n step S {} }")
			if err != nil {
				t.Fatalf("PipelineInput: parse: %v", err)
			}
			if got := stripSpaces(prog.Decls[0].Pipeline.Body[0].Input.Type.String()); got != stripSpaces(c.annotation) {
				t.Errorf("PipelineInput.Type.String() = %q, want %q", got, stripSpaces(c.annotation))
			}

			// Skill Field.
			prog, err = Parse("skill K { input { v: " + c.annotation + " } }")
			if err != nil {
				t.Fatalf("Field: parse: %v", err)
			}
			if got := stripSpaces(prog.Decls[0].Skill.Body[0].Input.Fields[0].Type.String()); got != stripSpaces(c.annotation) {
				t.Errorf("Field.Type.String() = %q, want %q", got, stripSpaces(c.annotation))
			}
		})
	}
}

// stripSpaces mirrors ast.TypeExpr.String()'s own rendering, which drops the
// spacing a hand-written annotation may use around "{"/":" — this helper
// lets the table above write natural-looking annotations while still
// comparing against the renderer's actual compact output.
func stripSpaces(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// TestTypeExprOptionalityPreserved confirms an untyped tool method param and
// an untyped method return (no ": Type" annotation at all) still parse with
// Type/Returns == nil, exactly as they did before *ast.TypeExpr existed —
// the grammar's optionality (`( ':' @@ )?`) must not have regressed.
func TestTypeExprOptionalityPreserved(t *testing.T) {
	prog, err := Parse(`
tool T {
    untyped(a, b) -> a + b
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := prog.Decls[0].Tool.Methods[0]
	if m.Returns != nil {
		t.Errorf("expected untyped method to have Returns == nil, got %#v", m.Returns)
	}
	for _, p := range m.Params {
		if p.Type != nil {
			t.Errorf("expected untyped param %q to have Type == nil, got %#v", p.Name, p.Type)
		}
	}
}

// TestTypeExprMalformedShapeYieldsParseError confirms an unbalanced "{" in a
// type-shape annotation still fails loudly at parse time rather than
// silently producing a partial tree.
func TestTypeExprMalformedShapeYieldsParseError(t *testing.T) {
	_, err := Parse(`
tool T {
    m(v: { name: string) -> v
}
`)
	if err == nil {
		t.Fatalf("expected a parse error for an unbalanced object shape")
	}
}
