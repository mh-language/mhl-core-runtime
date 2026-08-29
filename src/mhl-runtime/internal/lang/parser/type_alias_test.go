package parser

import "testing"

// TestTypeAliasDeclParses covers the `type X = <TypeExpr>` top-level
// declaration and confirms `type` stays a soft keyword (a variable named
// `type` still parses outside the declaration position).
func TestTypeAliasDeclParses(t *testing.T) {
	prog, err := Parse(`
type Slug  = string
type Ids   = string[]
type Point = { x: number, y: number }
pipeline P { step S { var type = 3 } }
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	var names []string
	for _, d := range prog.Decls {
		if d.Type != nil {
			names = append(names, d.Type.Name)
		}
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 type aliases, got %v", names)
	}
	if prog.Decls[0].Type.Type.String() != "string" {
		t.Errorf("Slug target = %q", prog.Decls[0].Type.Type.String())
	}
}
