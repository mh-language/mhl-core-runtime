package ast

// Agent declares an AI agent and its configuration (engine, credentials,
// resiliency, skills, tools, ...). The same node is reused for inline agent
// blocks (e.g. inside a `fallback: [...]` list), where the name is omitted.
type Agent struct {
	Name  string      `parser:"'agent' @Ident?"`
	Props []*Property `parser:"'{' @@* '}'"`
}
