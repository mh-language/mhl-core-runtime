package runtime

import "sort"

// InputSchema renders p's declared `input name: Type` members as a single
// JSON Schema object — the shape a server adapter (MCP tool `inputSchema`,
// A2A skill input schema) advertises and validates a request's arguments
// against before starting a run.
//
// Every declared input is required and no extra properties are allowed: the
// schema is the adapter's contract, deliberately tighter than `mhl run`'s
// own leniency toward unrecognised --input flags. A pipeline with no inputs
// yields {"type":"object","properties":{},"additionalProperties":false}.
func (p Pipeline) InputSchema() map[string]any {
	props := make(map[string]any, len(p.Inputs))
	required := make([]string, 0, len(p.Inputs))
	for _, in := range p.Inputs {
		props[in.Name] = in.Type.JSONSchema()
		required = append(required, in.Name)
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema
}
