package ast

import "github.com/alecthomas/participle/v2/lexer"

// Skill declares a modular skill: scoped tools/mcp_servers, typed input/output
// contracts, and system instructions.
type Skill struct {
	Name string         `parser:"'skill' @Ident"`
	Body []*SkillMember `parser:"'{' @@* '}'"`
}

// SkillMember is one entry of a skill body: an `input`/`output` field block or
// an ordinary property (description, tools, mcp_servers, system_instructions).
type SkillMember struct {
	Input  *FieldBlock `parser:"( 'input' @@"`
	Output *FieldBlock `parser:"| 'output' @@"`
	Prop   *Property   `parser:"| @@ )"`
}

// FieldBlock is a brace-delimited block of typed field declarations.
type FieldBlock struct {
	Fields []*Field `parser:"'{' @@* '}'"`
}

// Field is a typed field declaration, e.g. `target_file: string`.
type Field struct {
	Pos  lexer.Position
	Name string    `parser:"@Ident ':'"`
	Type *TypeExpr `parser:"@@"`
}
