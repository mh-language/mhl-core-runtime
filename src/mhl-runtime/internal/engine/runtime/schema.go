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
		// A stage input may be supplied up front, but a run never needs it to
		// start: it is asked for on entering its stage (InputGate).
		if in.Required() && in.Stage == "" {
			required = append(required, in.Name)
		}
		props[in.Name] = p.inputPropSchema(in)
	}
	schema := objectSchema(props, required)
	p.withJourney(schema)
	return schema
}

// withJourney annotates a staged pipeline's input schema with the order a
// run asks for its inputs — what a client guides a caller through, since a
// JSON object's properties carry no order:
//
//   - "x-mhl-order": every input's name, in declaration order;
//   - "x-mhl-stages": each named stage in run order, with the inputs it asks
//     for on entry ([] for none) — the workflow's own inputs are the rest,
//     asked for when the run starts.
//
// A pipeline without named stages gets neither.
func (p Pipeline) withJourney(schema map[string]any) {
	named := p.NamedStages()
	if len(named) == 0 {
		return
	}
	order := make([]string, 0, len(p.Inputs))
	for _, in := range p.Inputs {
		order = append(order, in.Name)
	}
	stages := make([]any, 0, len(named))
	for _, st := range named {
		inputs := []string{}
		for _, in := range p.StageInputs(st.Name) {
			inputs = append(inputs, in.Name)
		}
		stages = append(stages, map[string]any{"name": st.Name, "inputs": inputs})
	}
	schema["x-mhl-order"] = order
	schema["x-mhl-stages"] = stages
}

// StageInputSchema renders the inputs named in names (of stage stage) as
// one closed JSON Schema object, every one required — what a run paused at
// an InputGate waits for, and a client renders a form from.
func (p Pipeline) StageInputSchema(stage string, names []string) map[string]any {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	props := map[string]any{}
	var required []string
	for _, in := range p.StageInputs(stage) {
		if want[in.Name] {
			props[in.Name] = p.inputPropSchema(in)
			required = append(required, in.Name)
		}
	}
	s := objectSchema(props, required)
	s["title"] = stage
	return s
}

// inputPropSchema is one input's property schema in InputSchema: its type,
// enum variants, literal default and — for a stage input — the stage it is
// asked for in, as "x-mhl-stage".
func (p Pipeline) inputPropSchema(in PipelineInputSpec) map[string]any {
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
		} else if v, ok := enumDefault(in); ok {
			s["default"] = v
		}
	}
	if in.Stage != "" {
		s["x-mhl-stage"] = in.Stage
	}
	p.withEnumVariants(s)
	// The author's `///` doc is what a client shows beside the field — in
	// place of an enum's "mhl enum X" marker, whose variants are filled in by now.
	if in.Doc != "" {
		s["description"] = in.Doc
	}
	return s
}

// enumDefault reads an enum input's `= Enum.Variant` default as the bare
// variant name — the value a caller passes for it.
func enumDefault(in PipelineInputSpec) (string, bool) {
	if len(in.EnumVariants) == 0 {
		return "", false
	}
	pf := ast.BarePostfix(in.Default)
	if pf == nil || pf.Primary == nil || pf.Primary.Ident != in.Type.Name || len(pf.Ops) != 1 || pf.Ops[0].Member == "" {
		return "", false
	}
	for _, v := range in.EnumVariants {
		if v == pf.Ops[0].Member {
			return v, true
		}
	}
	return "", false
}

func objectSchema(props map[string]any, required []string) map[string]any {
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

// TypeSchema renders t — a type of this pipeline's program, such as a
// gate's decision type — as JSON Schema titled title, enum variants filled
// in at any depth.
func (p Pipeline) TypeSchema(t types.Type, title string) map[string]any {
	s := t.JSONSchema()
	if title != "" {
		s["title"] = title
	}
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
	// Stage is set when the inputs were checked on resuming a run paused at
	// that stage's InputGate (ValidateStageInputs): Missing are its inputs.
	Stage    string
	Missing  []string // declared inputs with no value supplied
	Unknown  []string // supplied keys the pipeline never declares
	Declared []string // every input the pipeline does declare (for the message)
}

func (e *InvalidInputsError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invalid inputs for %q", e.Pipeline)
	if e.Stage != "" {
		fmt.Fprintf(&b, " stage %q", e.Stage)
	}
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
		if in.Required() && in.Stage == "" {
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

// ValidateStageInputs is the admission check for resuming a run paused at
// stage's InputGate: every required input of the stage must now be bound,
// either already (vars, the checkpoint's variables) or by args. args may
// carry no undeclared key, as on a fresh run. It returns *InvalidInputsError
// (Stage set) when one is still missing, nil otherwise — the run then stays
// paused, its checkpoint untouched.
func (p Pipeline) ValidateStageInputs(stage string, args, vars map[string]any) error {
	declared := make(map[string]bool, len(p.Inputs))
	declaredList := make([]string, 0, len(p.Inputs))
	for _, in := range p.Inputs {
		declared[in.Name] = true
		declaredList = append(declaredList, in.Name)
	}
	var missing, unknown []string
	for _, name := range p.MissingStageInputs(stage, vars) {
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
	sort.Strings(unknown)
	sort.Strings(declaredList)
	return &InvalidInputsError{Pipeline: p.Name, Stage: stage, Missing: missing, Unknown: unknown, Declared: declaredList}
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

// WithBound returns inputs plus a workflow alias's bound values (Bound),
// which always win: they are not part of the alias's input contract, so a
// caller never legitimately supplies one. inputs is not modified.
func (pipeline Pipeline) WithBound(inputs map[string]any) map[string]any {
	if len(pipeline.Bound) == 0 {
		return inputs
	}
	out := make(map[string]any, len(inputs)+len(pipeline.Bound))
	for k, v := range inputs {
		out[k] = v
	}
	for k, v := range pipeline.Bound {
		out[k] = v
	}
	return out
}

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
	declared := make(map[string]bool, len(pipeline.Inputs)+len(pipeline.Bound))
	for _, in := range pipeline.Inputs {
		declared[in.Name] = true
	}
	for name := range pipeline.Bound { // a workflow alias's fixed fields
		declared[name] = true
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
