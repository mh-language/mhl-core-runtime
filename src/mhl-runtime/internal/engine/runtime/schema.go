package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// InputSchema renders p's declared `input name: Type` members as a single
// JSON Schema object — the shape a server adapter (MCP tool `inputSchema`,
// A2A skill input schema) advertises and validates a request's arguments
// against before starting a run.
//
// Every declared input is required and no extra properties are allowed,
// unless the input carries a `= expr` default (PipelineInputSpec.Default)
// or is an optional `name?: T` field of a typed signature's param type,
// in which case it is left out of "required" and — best-effort, when the
// default is itself a literal (ast.LiteralValue) — its "default" key is
// filled in too; a non-literal default (e.g. one reading `context.*`) still
// makes the input optional, it just has no representable JSON default. An
// enum-typed input (PipelineInputSpec.EnumVariants) gets its declared
// variant list as a proper JSON Schema `"enum"` array — Type.JSONSchema()
// can't do this on its own (an EnumKind Type carries only its name, not its
// variants; see that method's own doc comment), so this is the one place
// with access to both. The schema is otherwise the adapter's contract,
// deliberately tighter than `mhl run`'s own leniency toward unrecognised
// --input flags. A pipeline with no inputs yields
// {"type":"object","properties":{},"additionalProperties":false}.
func (p Pipeline) InputSchema() map[string]any {
	props := make(map[string]any, len(p.Inputs))
	required := make([]string, 0, len(p.Inputs))
	for _, in := range p.Inputs {
		s := in.Type.JSONSchema()
		if len(in.EnumVariants) > 0 {
			variants := make([]any, len(in.EnumVariants))
			for i, v := range in.EnumVariants {
				variants[i] = v
			}
			s["enum"] = variants
		}
		if in.Default != nil {
			if dv, ok := ast.LiteralValue(in.Default); ok {
				s["default"] = dv
			}
		}
		if in.Required() {
			required = append(required, in.Name)
		}
		p.withEnumVariants(s)
		props[in.Name] = s
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

// OutputSchema renders the declared result type of a typed signature
// (`workflow X(...): ReviewOutput`) as JSON Schema — what an MCP tool
// advertises as `outputSchema`, titled with the type as written. nil when
// the pipeline declares no result type, or declares one that is not an
// object (MCP requires an object root; lint rejects a non-object result
// type anyway, since `output:` is always an object mapping).
func (p Pipeline) OutputSchema() map[string]any {
	if p.OutputType == nil || p.OutputType.Kind != types.ObjectKind {
		return nil
	}
	s := p.OutputType.JSONSchema()
	s["title"] = p.OutputTypeName
	p.withEnumVariants(s)
	return s
}

// withEnumVariants fills in `"enum": [...]` wherever s — at any depth,
// through object properties and array items — holds the enum placeholder
// Type.JSONSchema emits (`{"type":"string","description":"mhl enum X"}`)
// for an enum this pipeline's program declares.
func (p Pipeline) withEnumVariants(s map[string]any) {
	if name, ok := strings.CutPrefix(fmt.Sprint(s["description"]), "mhl enum "); ok && s["enum"] == nil {
		if variants, ok := p.Enums[name]; ok {
			list := make([]any, len(variants))
			for i, v := range variants {
				list[i] = v
			}
			s["enum"] = list
		}
	}
	if props, ok := s["properties"].(map[string]any); ok {
		for _, v := range props {
			if sub, ok := v.(map[string]any); ok {
				p.withEnumVariants(sub)
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		p.withEnumVariants(items)
	}
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
	required := make(map[string]bool, len(p.Inputs))
	for _, in := range p.Inputs {
		declared[in.Name] = true
		declaredList = append(declaredList, in.Name)
		if in.Required() {
			required[in.Name] = true
		}
	}
	var missing, unknown []string
	for _, name := range declaredList {
		if _, ok := args[name]; !ok && required[name] {
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

// OutputContractError reports a finished run whose `output:` projection does
// not satisfy the pipeline's declared result type (`workflow X(...): T`).
// The run itself completed — every step ran — but its result breaks the
// contract the pipeline advertises (MCP `outputSchema`), so it is returned
// to the caller as an error, never as a result.
type OutputContractError struct {
	Pipeline string
	Type     string // the declared type, as written
	Err      error  // the types.Check mismatch
}

func (e *OutputContractError) Error() string {
	return fmt.Sprintf("output of %q does not satisfy %s: %v", e.Pipeline, e.Type, e.Err)
}

func (e *OutputContractError) Unwrap() error { return e.Err }

// BindInputs injects a run's coerced inputs into a step's variable env —
// once per step, so a resumed run sees them too and a supplied value always
// wins over an `input x: T = default` seed.
//
// The per-line form binds each input as its own var. A typed signature
// (`workflow X(req: T)`) binds its declared fields as one object under the
// param name instead, merged onto whatever that object already holds (a
// resumed run's checkpointed `req`, when the resume supplies no inputs or
// only some). A key the pipeline does not declare — a pause/resume
// decision merged in by run/resume — still binds as a top-level var in
// both forms. Every declared field is present on the object; an omitted
// optional one is null.
func (pipeline Pipeline) BindInputs(vars, inputs map[string]any) {
	if pipeline.InputParam == "" {
		for k, v := range inputs {
			vars[k] = v
		}
		return
	}
	declared := make(map[string]bool, len(pipeline.Inputs))
	for _, in := range pipeline.Inputs {
		declared[in.Name] = true
	}
	param := map[string]any{}
	if prev, ok := vars[pipeline.InputParam].(map[string]any); ok {
		for k, v := range prev {
			param[k] = v
		}
	}
	for k, v := range inputs {
		if declared[k] {
			param[k] = v
		} else {
			vars[k] = v
		}
	}
	// An omitted optional field reads as null, not "field not found", so
	// `req.base ?? "main"` is how a step spells its default.
	for name := range declared {
		if _, ok := param[name]; !ok {
			param[name] = nil
		}
	}
	vars[pipeline.InputParam] = param
}
