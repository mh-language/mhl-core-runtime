package extension

import "fmt"

// Value is any value that crosses the host/extension boundary. It is
// constrained to the JSON-compatible shapes the mhl runtime already uses for
// dynamic values: nil, bool, float64, string, []any, map[string]any.
type Value = any

// Position is a 1-based source location. It mirrors participle/lexer.Position
// without importing it, so the contract stays free of parser types.
type Position struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// Property is one resolved `key: value` pair from a declaration body. The
// core evaluates the value expression before handing it over — an extension
// never sees an unevaluated AST expression.
type Property struct {
	Name  string   `json:"name"`
	Value Value    `json:"value"`
	Pos   Position `json:"pos,omitempty"`
}

// Declaration is a parsed `extension <kind> <Name> { ... }` construct with
// its properties resolved to values. It is the only view of the program an
// extension receives.
type Declaration struct {
	Kind  string     `json:"kind"`
	Name  string     `json:"name"`
	Props []Property `json:"props,omitempty"`
	Pos   Position   `json:"pos,omitempty"`
}

// Prop returns the value of the named property and whether it was present.
func (d Declaration) Prop(name string) (Value, bool) {
	for _, p := range d.Props {
		if p.Name == name {
			return p.Value, true
		}
	}
	return nil, false
}

// StringProp returns the named property as a string. ok is false when the
// property is absent or is not a string.
func (d Declaration) StringProp(name string) (value string, ok bool) {
	v, present := d.Prop(name)
	if !present {
		return "", false
	}
	s, isString := v.(string)
	return s, isString
}

// PropertySpec describes one property a declaration kind accepts. Consumed by
// lint and by LSP completion.
type PropertySpec struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Required      bool   `json:"required,omitempty"`
	Documentation string `json:"documentation,omitempty"`
}

// DeclarationSpec describes one declaration kind an extension serves. Methods
// is the static set of operations a bound Instance of this kind exposes —
// carried here (not only on Instance.Methods) so lint and the LSP can read it
// without binding an instance.
type DeclarationSpec struct {
	Kind          string         `json:"kind"`
	Documentation string         `json:"documentation,omitempty"`
	Properties    []PropertySpec `json:"properties,omitempty"`
	Methods       []MethodSpec   `json:"methods,omitempty"`
}

// ParamSpec is one method parameter.
type ParamSpec struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

// MethodSpec describes one callable operation. Signature and Documentation
// feed LSP hovers and signature help.
type MethodSpec struct {
	Name          string      `json:"name"`
	Params        []ParamSpec `json:"params,omitempty"`
	Returns       string      `json:"returns,omitempty"`
	Signature     string      `json:"signature,omitempty"`
	Documentation string      `json:"documentation,omitempty"`
}

// CallRequest is one method invocation forwarded from `.mh` code. Positional
// and named arguments mirror how mhl call sites bind: a method reads whichever
// its own signature dictates.
type CallRequest struct {
	Declaration Declaration      `json:"declaration"`
	Method      string           `json:"method"`
	Args        []Value          `json:"args,omitempty"`
	NamedArgs   map[string]Value `json:"named_args,omitempty"`
}

// Arg returns the i-th positional argument and whether it was supplied.
func (r CallRequest) Arg(i int) (Value, bool) {
	if i < 0 || i >= len(r.Args) {
		return nil, false
	}
	return r.Args[i], true
}

// Named returns the named argument and whether it was supplied.
func (r CallRequest) Named(name string) (Value, bool) {
	v, ok := r.NamedArgs[name]
	return v, ok
}

// StringArg reads a string argument by name, falling back to positional slot
// pos. ok is false when neither was supplied or the value is not a string.
// It mirrors the interpreter's callArgs.stringNamedOrAt so an adapter parses
// call arguments exactly as the old dedicated dispatch did.
func (r CallRequest) StringArg(name string, pos int) (string, bool) {
	if v, ok := r.NamedArgs[name]; ok {
		s, ok := v.(string)
		return s, ok
	}
	v, ok := r.Arg(pos)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// IntArg reads an integer argument by name or positional slot pos. mhl has a
// single float64 number type, so a whole-valued float64 counts. ok is false
// when the value is missing or not numeric.
func (r CallRequest) IntArg(name string, pos int) (int, bool) {
	v, ok := r.NamedArgs[name]
	if !ok {
		if v, ok = r.Arg(pos); !ok {
			return 0, false
		}
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// ObjectArg reads an object argument by name or positional slot pos. ok is
// false when the value is missing; err is set when it was supplied but is not
// an object, so a caller can report the same "must be an object" failure the
// old dispatch did.
func (r CallRequest) ObjectArg(name string, pos int) (obj map[string]any, ok bool, supplied bool) {
	v, has := r.NamedArgs[name]
	if !has {
		if v, has = r.Arg(pos); !has {
			return nil, false, false
		}
	}
	m, isObj := v.(map[string]any)
	return m, isObj, true
}

// Severity classifies a Diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is a static-analysis finding an extension reports for one
// declaration. Code is a stable, machine-readable identifier.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Code     string   `json:"code,omitempty"`
	Pos      Position `json:"pos,omitempty"`
}

// Errorf builds an error-severity Diagnostic anchored at pos.
func Errorf(pos Position, code, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: SeverityError, Code: code, Pos: pos, Message: fmt.Sprintf(format, args...)}
}
