package runtime

import (
	"fmt"
	"sort"
	"strings"
)

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

// InvalidInputsError reports arguments that do not satisfy a pipeline's
// InputSchema: a required input left unset, or a property the pipeline never
// declares (the schema is closed — additionalProperties:false). A server
// adapter maps it to a JSON-RPC -32602; `mhl run` prints it and exits
// non-zero. Both slices are sorted and at least one is non-empty.
type InvalidInputsError struct {
	Pipeline string
	Missing  []string // declared inputs with no value supplied
	Unknown  []string // supplied keys the pipeline never declares
	Declared []string // every input the pipeline does declare (for the message)
}

func (e *InvalidInputsError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invalid inputs for %q", e.Pipeline)
	sep := ": "
	if len(e.Missing) > 0 {
		fmt.Fprintf(&b, "%smissing required input %s", sep, quoteList(e.Missing))
		sep = "; "
	}
	if len(e.Unknown) > 0 {
		fmt.Fprintf(&b, "%sundeclared input %s", sep, quoteList(e.Unknown))
	}
	if len(e.Declared) == 0 {
		b.WriteString(" (pipeline declares no inputs)")
	} else {
		fmt.Fprintf(&b, " (declared: %s)", quoteList(e.Declared))
	}
	return b.String()
}

// ValidateInputs enforces the contract InputSchema() advertises: every
// declared input must be present in args, and args may carry no key the
// pipeline does not declare. It returns *InvalidInputsError when either rule
// is broken, nil otherwise. Type coercion of the values that are present is a
// separate, later step (execsvc); this is the admission check that runs
// before anything is created.
//
// It is intentionally unconditional — there is no "lenient" mode. A caller
// resuming a run skips this entirely (the checkpoint, not the request, is the
// source of truth for inputs on a resume).
func (p Pipeline) ValidateInputs(args map[string]any) error {
	declared := make(map[string]bool, len(p.Inputs))
	declaredList := make([]string, 0, len(p.Inputs))
	for _, in := range p.Inputs {
		declared[in.Name] = true
		declaredList = append(declaredList, in.Name)
	}
	var missing, unknown []string
	for _, name := range declaredList {
		if _, ok := args[name]; !ok {
			missing = append(missing, name)
		}
	}
	for k := range args {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(missing) == 0 && len(unknown) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	sort.Strings(declaredList)
	return &InvalidInputsError{Pipeline: p.Name, Missing: missing, Unknown: unknown, Declared: declaredList}
}

func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(q, ", ")
}
