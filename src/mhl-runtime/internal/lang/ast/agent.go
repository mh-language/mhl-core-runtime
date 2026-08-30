package ast

import "github.com/alecthomas/participle/v2/lexer"

// Agent declares an AI agent and its configuration (engine, credentials,
// resiliency, before/after hooks, ...). The same node is reused for inline agent
// blocks (e.g. inside a `fallback: [...]` list), where the name is omitted.
type Agent struct {
	Pos   lexer.Position
	Name  string      `parser:"'agent' @Ident?"`
	Props []*Property `parser:"'{' @@* '}'"`
}
