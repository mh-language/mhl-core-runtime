package ast

import "github.com/alecthomas/participle/v2/lexer"

// Agent declares an AI agent and its configuration (engine, credentials,
// resiliency, before/after hooks, ...). The same node is reused for inline agent
// blocks (e.g. inside a `fallback: [...]` list), where the name is omitted.
//
// Inputs are the agent's per-call parameters — `input repo: string`, `input
// model: string = ""` — with the same syntax as a workflow's `input`
// (PipelineInput) and declared anywhere among the properties. A
// `.run(prompt: ..., repo: "/abs")` call fills them by name; a missing one
// without a default is a call error. They are bound for the agent's own
// expressions (args, cwd, stdin, log, timeout, the before/after hooks, ...)
// — see interpreter.bindAgentInputs. Tagged `digest:"omitzero"` so an agent
// without inputs keeps the DefinitionDigest an older build stamped.
type Agent struct {
	Pos    lexer.Position
	Name   string           `parser:"'agent' @Ident? '{'"`
	Inputs []*PipelineInput `parser:"( 'input' @@" digest:"omitzero"`
	Props  []*Property      `parser:"| @@ )* '}'"`
}

// InputNames returns the agent's declared input names, in declaration order.
func (a *Agent) InputNames() []string {
	names := make([]string, len(a.Inputs))
	for i, in := range a.Inputs {
		names[i] = in.Name
	}
	return names
}

// FindInput returns the agent's declared input named name.
func (a *Agent) FindInput(name string) (*PipelineInput, bool) {
	for _, in := range a.Inputs {
		if in.Name == name {
			return in, true
		}
	}
	return nil, false
}
