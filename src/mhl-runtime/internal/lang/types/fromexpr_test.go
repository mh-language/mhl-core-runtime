package types

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// parseTypeExpr parses annotation as a tool method's return-type annotation
// (the simplest existing grammar site) and returns the parsed *ast.TypeExpr —
// reused across every FromExpr test case so each case only has to write the
// annotation text, not a whole program.
func parseTypeExpr(t *testing.T, annotation string) *ast.TypeExpr {
	t.Helper()
	prog, err := parser.Parse("tool T { m(): " + annotation + " -> 1 }")
	if err != nil {
		t.Fatalf("parse %q: %v", annotation, err)
	}
	return prog.Decls[0].Tool.Methods[0].Returns
}

func TestFromExpr(t *testing.T) {
	cases := []struct {
		name       string
		annotation string
		want       Type
		wantOk     bool
	}{
		{"bare keyword", "string", String, true},
		{"alias", "int", Number, true},
		{"unrecognized bare keyword", "sting", Type{}, false},
		{"single array suffix", "string[]", ArrayOf(String), true},
		{"nested array", "string[][]", ArrayOf(ArrayOf(String)), true},
		{"simple object shape", "{ name: string, age: number }",
			ObjectOf(map[string]Type{"name": String, "age": Number}), true},
		{"nested object shape", "{ meta: { active: bool } }",
			ObjectOf(map[string]Type{"meta": ObjectOf(map[string]Type{"active": Bool})}), true},
		{"mixed shape+array fields", "{ tags: string[], meta: { active: bool } }",
			ObjectOf(map[string]Type{
				"tags": ArrayOf(String),
				"meta": ObjectOf(map[string]Type{"active": Bool}),
			}), true},
		{"array of shapes", "{ name: string }[]",
			ArrayOf(ObjectOf(map[string]Type{"name": String})), true},
		{"unrecognized keyword inside nested shape", "{ age: sting }", Type{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr := parseTypeExpr(t, c.annotation)
			got, ok := FromExpr(expr)
			if ok != c.wantOk {
				t.Fatalf("FromExpr(%q) ok = %v, want %v", c.annotation, ok, c.wantOk)
			}
			if ok && !got.Equal(c.want) {
				t.Errorf("FromExpr(%q) = %v, want %v", c.annotation, got, c.want)
			}
		})
	}
}

func TestFromExprNil(t *testing.T) {
	got, ok := FromExpr(nil)
	if !ok || !got.Equal(Any) {
		t.Errorf("FromExpr(nil) = %v, %v, want Any, true", got, ok)
	}
}

func TestFromExprDuplicateField(t *testing.T) {
	expr := parseTypeExpr(t, "{ name: string, name: number }")
	if _, ok := FromExpr(expr); ok {
		t.Errorf("expected ok=false for a shape with a duplicate field name")
	}
}
