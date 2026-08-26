// Package ast defines the Go AST node types for the Meta-Harness Language
// (.mh). The struct tags encode the Participle v2 grammar; see
// internal/parser for how these types are assembled into a parser.
//
// Scope: parsing/AST only. These types describe the shape of a parsed .mh
// program and carry no runtime-evaluation behavior.
package ast

import "github.com/alecthomas/participle/v2/lexer"

// Program is the root node: a whole .mh source file is a sequence of
// top-level declarations.
type Program struct {
	Decls []*Declaration `parser:"@@*"`
	// Aliases is populated while imports are resolved. It is intentionally
	// outside the source grammar: aliases are bindings for the flattened
	// program namespace, not declarations in the source AST.
	aliases map[string]string
}

// AliasMap returns the resolved local aliases attached to the program. The
// map is runtime metadata and is therefore deliberately not parsed from
// source.
func (p *Program) AliasMap() map[string]string {
	if p.aliases == nil {
		p.aliases = map[string]string{}
	}
	return p.aliases
}

// Declaration is any top-level construct. An optional leading `export`
// keyword may precede a declaration to mark it as exported from the module.
type Declaration struct {
	Export    bool       `parser:"@'export'?"`
	Import    *Import    `parser:"( @@"`
	Use       *Use       `parser:"| @@"`
	Skill     *Skill     `parser:"| @@"`
	Prompt    *Prompt    `parser:"| @@"`
	MCPServer *MCPServer `parser:"| @@"`
	Agent     *Agent     `parser:"| @@"`
	Memory    *Memory    `parser:"| @@"`
	Tool      *Tool      `parser:"| @@"`
	Pipeline  *Pipeline  `parser:"| @@"`
	Test      *Test      `parser:"| @@ )"`
}

// Import binds another module under a local alias:
//
//	import "./agentes/qualidade.mh" as qa
type Import struct {
	Pos   lexer.Position
	Path  string `parser:"'import' @String"`
	Alias string `parser:"'as' @Ident"`
}

// Use selectively imports named symbols from another module. Each imported
// symbol may optionally receive a local alias:
//
//	use { SecurityAudit as audit } from "./prompts/seguranca.mh"
type Use struct {
	Pos   lexer.Position
	Items []*UseItem `parser:"'use' '{' @@ ( ',' @@ )* '}'"`
	Path  string     `parser:"'from' @String"`
}

// UseItem is one selectively imported symbol and its optional local alias.
type UseItem struct {
	Name  string `parser:"@Ident"`
	Alias string `parser:"( 'as' @Ident )?"`
}

// Names returns the source names in a use clause. Keeping this helper avoids
// making import diagnostics and resolution logic depend on the AST layout.
func (u *Use) Names() []string {
	names := make([]string, 0, len(u.Items))
	for _, item := range u.Items {
		names = append(names, item.Name)
	}
	return names
}

// MCPServer declares a stateless MCP server endpoint.
type MCPServer struct {
	Name  string      `parser:"'mcp_server' @Ident"`
	Props []*Property `parser:"'{' @@* '}'"`
}

// Memory declares a memory backend (kv, vector, ...).
type Memory struct {
	Name  string      `parser:"'memory' @Ident"`
	Props []*Property `parser:"'{' @@* '}'"`
}

// Tool declares a namespace of native tool methods.
type Tool struct {
	Name    string        `parser:"'tool' @Ident"`
	Methods []*ToolMethod `parser:"'{' @@* '}'"`
}

// ToolMethod is a single native method mapping, either a single expression:
//
//	read_file(path: string) -> fs.read(path)
//
// or a full statement block with an optional `return` (void, implicitly
// `nil`, if the block never returns):
//
//	count(items) -> {
//	    var n = 0
//	    while (n < items.size()) { n = n + 1 }
//	    return n
//	}
//
// Body is tried first, so a block that's actually a single object-literal
// expression (`-> {a: 1}`) still parses as Body, not Block — Block only
// matches when the content inside `{ }` isn't shaped like `key: value`
// pairs (see internal/parser's TestToolMethodBlockBody* for the
// disambiguation this relies on).
// Returns is a typed method declaration's optional return-type annotation,
// e.g. `read_file(path: string): string -> fs.read(path)` — the same `:
// Ident` shape Param already uses, placed after the closing ')' since a
// method's return type describes the whole call, not one parameter.
type ToolMethod struct {
	Pos     lexer.Position
	Name    string       `parser:"@Ident '('"`
	Params  []*Param     `parser:"( @@ ( ',' @@ )* )? ')'"`
	Returns *TypeExpr    `parser:"( ':' @@ )?"`
	Body    *Expr        `parser:"'->' ( @@"`
	Block   []*Statement `parser:"| '{' @@* '}' )"`
}

// Param is a typed parameter declaration, e.g. `path: string`.
type Param struct {
	Pos  lexer.Position
	Name string    `parser:"@Ident"`
	Type *TypeExpr `parser:"( ':' @@ )?"`
}

// Property is a `key: value` pair inside a declaration body.
type Property struct {
	Pos   lexer.Position
	Name  string `parser:"@Ident ':'"`
	Value *Expr  `parser:"@@"`
}
