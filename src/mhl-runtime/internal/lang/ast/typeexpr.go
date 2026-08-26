package ast

import (
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// TypeExpr is the grammar every `: Type` annotation site (ast.Param.Type,
// ast.PipelineInput.Type, ast.Field.Type, ast.ToolMethod.Returns) parses
// into, replacing what used to be a bare Ident string. It is either a bare
// keyword name (`string`, `number`, ..., or any other Ident text — e.g. the
// "prompt" pseudo-type a Prompt.Param may carry, which internal/lang/types
// never resolves at all — see internal/lang/lint's
// TestCheckToolCallPromptParamsNotSwept) or an inline anonymous object
// field-shape contract (`{ name: string, age: number }`), and either of
// those may carry one or more trailing `[]` array-element suffixes:
// `string[]`, `string[][]`, `{ name: string }[]`.
//
// This is a type-level grammar, distinct from (and never mixed into) Expr's
// own Object/Array *value*-literal grammar (expr.go): a TypeExpr describes
// what shape a value must have, never a value itself, and is never
// evaluated.
type TypeExpr struct {
	Pos   lexer.Position
	Name  string       `parser:"( @Ident"`
	Shape *ObjectShape `parser:"| @@ )"`
	// ArraySuffixes captures one string per trailing `[]` — its length IS
	// the array nesting depth (`string[][]` -> len 2). Each captured string
	// is always literally "]"; only the count matters (see
	// internal/lang/types.FromExpr for how this peels one level of
	// Array-wrapping per suffix).
	ArraySuffixes []string `parser:"( '[' @']' )*"`
}

// ObjectShape is the inline anonymous field-shape contract embedded
// directly in a type annotation. Unlike Object (expr.go's map/object value
// literal), every field here is itself a TypeExpr, never an Expr — these
// are type contracts, not values.
type ObjectShape struct {
	Pos    lexer.Position
	Fields []*ShapeField `parser:"'{' ( @@ ( ','? @@ )* ','? )? '}'"`
}

// ShapeField is one `name: TypeExpr` entry of an ObjectShape.
type ShapeField struct {
	Pos  lexer.Position
	Name string    `parser:"@Ident ':'"`
	Type *TypeExpr `parser:"@@"`
}

// String reconstructs e's surface syntax exactly as written (e.g. "sting[]",
// "{ name: sting }") — deliberately NOT the resolved/canonical rendering
// internal/lang/types.Type.String() produces (which would show a typo's
// containing shape post-alias-resolution and lose the exact text an
// "unrecognized type" diagnostic needs to quote back at the user). Because
// TypeExpr implements Stringer, every existing fmt.Sprintf(..., "%q",
// x.Type)-shaped diagnostic call site keeps working unchanged even though
// x.Type's static type changed from string to *TypeExpr. nil-safe: Param.Type
// and ToolMethod.Returns are optional and may be nil.
func (e *TypeExpr) String() string {
	if e == nil {
		return ""
	}
	base := e.Name
	if e.Shape != nil {
		base = e.Shape.String()
	}
	for range e.ArraySuffixes {
		base += "[]"
	}
	return base
}

// String reconstructs s's surface syntax, field order preserved as written
// (unlike types.Type.String(), which sorts field names for deterministic
// output) — this renderer's whole job is showing the user exactly what they
// typed.
func (s *ObjectShape) String() string {
	if s == nil {
		return "{}"
	}
	parts := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		parts[i] = f.Name + ": " + f.Type.String()
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}
