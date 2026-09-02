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
	// aliases is populated while `import { Name as Alias } from "..."` items are
	// resolved. It is intentionally outside the source grammar: aliases are
	// bindings for the flattened program namespace, not declarations in the
	// source AST.
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
	Prompt    *Prompt    `parser:"| @@"`
	Extension *Extension `parser:"| @@"`
	Agent     *Agent     `parser:"| @@"`
	Memory    *Memory    `parser:"| @@"`
	Tool      *Tool      `parser:"| @@"`
	Pipeline  *Pipeline  `parser:"| @@"`
	Type      *TypeAlias `parser:"| @@"`
	Enum      *Enum      `parser:"| @@"`
	Test      *Test      `parser:"| @@ )"`
}

// Enum declares a closed set of named constants:
//
//	enum Status { Draft, Published, Archived }
//
// It is intentionally minimal — a variant carries no payload and no
// associated value. A value of the enum is produced only by qualified
// access (`Status.Draft`); at run time it is a distinct tagged value, not a
// plain string (`Status.Draft == "Draft"` is false). `match` over an enum
// value is checked for exhaustiveness by `mhl lint`. Variants are separated
// by commas or newlines, with an optional trailing comma.
type Enum struct {
	Pos      lexer.Position
	Name     string   `parser:"'enum' @Ident '{'"`
	Variants []string `parser:"( @Ident ( ','? @Ident )* ','? )? '}'"`
}

// TypeAlias binds a name to a type expression so a long or repeated shape can
// be written once and referred to by name everywhere a `: Type` annotation is
// accepted (pipeline inputs, tool-method params and returns, object-shape
// fields):
//
//	type Violation = { code: string, severity: string }
//	type Ids       = string[]
//
// The alias is purely a spelling: internal/lang/types.Aliases resolves it to
// the same types.Type the target expression would produce inline, so an
// alias never adds a distinct type — a value satisfying `Ids` is exactly a
// value satisfying `string[]`. Aliases may reference other aliases (and, from
// the enum work, enum names); a cycle, a duplicate name, an unknown target,
// or shadowing a builtin keyword is a static error.
type TypeAlias struct {
	Pos  lexer.Position
	Name string    `parser:"'type' @Ident '='"`
	Type *TypeExpr `parser:"@@"`
}

// Import selectively imports named symbols from another module. Each imported
// symbol may optionally receive a local alias:
//
//	import { SecurityAudit as audit } from "./prompts/seguranca.mh"
type Import struct {
	Pos   lexer.Position
	Items []*ImportItem `parser:"'import' '{' @@ ( ',' @@ )* '}'"`
	Path  string        `parser:"'from' @String"`
}

// ImportItem is one selectively imported symbol and its optional local alias.
type ImportItem struct {
	Name  string `parser:"@Ident"`
	Alias string `parser:"( 'as' @Ident )?"`
}

// Names returns the source names in an import clause. Keeping this helper avoids
// making import diagnostics and resolution logic depend on the AST layout.
func (u *Import) Names() []string {
	names := make([]string, 0, len(u.Items))
	for _, item := range u.Items {
		names = append(names, item.Name)
	}
	return names
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

// Param is a typed parameter declaration, e.g. `path: string`. It may carry
// a default value — `mode: string = "0644"` — used when a caller omits the
// corresponding argument (tool methods and lambdas bind positionally, so a
// defaulted param must not be followed by a non-defaulted one; `prompt`
// binds by name, so its defaulted params may appear in any order). Default
// is nil when none was written. The default expression is evaluated lazily,
// once per omitting call, in the callee's own scope (see
// internal/engine/interpreter's evalToolCall / invokeClosureWithValues /
// renderPromptCallDynamic).
type Param struct {
	Pos     lexer.Position
	Name    string    `parser:"@Ident"`
	Type    *TypeExpr `parser:"( ':' @@ )?"`
	Default *Expr     `parser:"( '=' @@ )?"`
}

// RequiredParamCount is how many leading arguments a positional caller
// (tool method, lambda) must supply: every parameter up to the first one
// that declares a default. It assumes defaults are contiguous and trailing
// — a rule lint (checkParamDefaults) enforces — so the first defaulted
// parameter's index is the count.
func RequiredParamCount(params []*Param) int {
	for i, p := range params {
		if p.Default != nil {
			return i
		}
	}
	return len(params)
}

// Property is a `key: value` pair inside a declaration body.
type Property struct {
	Pos   lexer.Position
	Name  string `parser:"@Ident ':'"`
	Value *Expr  `parser:"@@"`
}
