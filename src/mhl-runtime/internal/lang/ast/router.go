package ast

import "github.com/alecthomas/participle/v2/lexer"

// Router declares a set of already-declared `agent`s and a decision policy
// for picking one of them at call time (`.delegate(prompt: ...)`). Unlike
// Agent, a Router always requires a name — there is no inline-literal form.
type Router struct {
	Pos   lexer.Position
	Name  string      `parser:"'router' @Ident"`
	Props []*Property `parser:"'{' @@* '}'"`
}
