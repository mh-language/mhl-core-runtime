// Package types is mhl's type vocabulary for gradual/optional static typing.
// It has no dependency outside internal/lang — it depends on
// internal/lang/ast (to resolve a parsed *ast.TypeExpr into this vocabulary,
// see FromExpr) but nothing from internal/engine, internal/features, or
// internal/cli, so both internal/lang/lint (static checking) and
// internal/engine/interpreter (runtime enforcement) can still import it
// without violating the repo's one-way layering order.
package types

import (
	"fmt"
	"sort"
	"strings"
)

// Kind is the closed tag of what shape of Type this is — mirroring
// internal/engine/interpreter's typeName runtime-value vocabulary
// (string/number/bool/array/object) plus Any, the untyped/dynamic default
// every var and unannotated param already behaves as today.
type Kind int

const (
	AnyKind Kind = iota
	StringKind
	NumberKind
	BoolKind
	ArrayKind
	ObjectKind
)

// Type is mhl's statically-checkable type vocabulary. Kind says what shape
// of value this is; Elem/Fields optionally refine an Array/Object with
// structure the vocabulary had no way to express before array-element and
// object-shape types existed:
//
//   - Elem is non-nil only when Kind == ArrayKind. A nil Elem means "array of
//     unknown/any element type" — the same permissive behavior a bare
//     `: array` annotation always had.
//   - Fields is non-nil only when Kind == ObjectKind. A nil Fields map means
//     "any-shaped object" — the same permissive behavior a bare `: object`
//     annotation always had.
//
// The zero Type{} is Any (Kind defaults to AnyKind, Elem/Fields both nil) —
// the same permissive default an unannotated param already behaves as.
//
// Type is deliberately not comparable with == once Fields is populated (Go
// maps aren't comparable) — use Equal for structural comparison anywhere
// code used to compare two Type values directly.
type Type struct {
	Kind   Kind
	Elem   *Type
	Fields map[string]Type
}

// Any/String/Number/Bool/Array/Object are the unshaped base values for each
// Kind — Array/Object here mean "no declared element type / field shape",
// exactly matching a bare `: array` / `: object` annotation. Use ArrayOf/
// ObjectOf to build a shaped Type.
var (
	Any    = Type{Kind: AnyKind}
	String = Type{Kind: StringKind}
	Number = Type{Kind: NumberKind}
	Bool   = Type{Kind: BoolKind}
	Array  = Type{Kind: ArrayKind}
	Object = Type{Kind: ObjectKind}
)

// ArrayOf builds a Type describing an array whose elements must satisfy elem.
func ArrayOf(elem Type) Type {
	return Type{Kind: ArrayKind, Elem: &elem}
}

// ObjectOf builds a Type describing an object whose declared fields must be
// present with the given types — see Check's Object case for the permissive
// (structural, not exact) matching rule this enables.
func ObjectOf(fields map[string]Type) Type {
	return Type{Kind: ObjectKind, Fields: fields}
}

// Equal reports whether t and other describe the same declared shape,
// structurally — the == replacement now that Type may hold a map. An
// unshaped Array/Object (Elem/Fields nil) is never Equal to a shaped one:
// that asymmetry is deliberate, see mergeVarType's use of Equal for the
// monotonic-downgrade-to-Any inference rule this backs.
func (t Type) Equal(other Type) bool {
	if t.Kind != other.Kind {
		return false
	}
	switch t.Kind {
	case ArrayKind:
		if (t.Elem == nil) != (other.Elem == nil) {
			return false
		}
		if t.Elem == nil {
			return true
		}
		return t.Elem.Equal(*other.Elem)
	case ObjectKind:
		if (t.Fields == nil) != (other.Fields == nil) {
			return false
		}
		if len(t.Fields) != len(other.Fields) {
			return false
		}
		for k, v := range t.Fields {
			ov, ok := other.Fields[k]
			if !ok || !v.Equal(ov) {
				return false
			}
		}
		return true
	default:
		return true // Kind already matched; String/Number/Bool/Any have no further structure
	}
}

// String renders t's resolved/canonical spelling for error messages —
// "string", "string[]", "string[][]", "{age: number, name: string}" (field
// names sorted for deterministic output, since Go map iteration order is
// randomized). This is the resolved/canonical rendering (aliases like "int"
// always print as "number"); see ast.TypeExpr.String() for the surface-
// syntax renderer used when a diagnostic must quote back exactly what the
// user typed (e.g. an unrecognized-type typo).
func (t Type) String() string {
	switch t.Kind {
	case StringKind:
		return "string"
	case NumberKind:
		return "number"
	case BoolKind:
		return "bool"
	case ArrayKind:
		if t.Elem == nil {
			return "array"
		}
		return t.Elem.String() + "[]"
	case ObjectKind:
		if t.Fields == nil {
			return "object"
		}
		names := make([]string, 0, len(t.Fields))
		for k := range t.Fields {
			names = append(names, k)
		}
		sort.Strings(names)
		parts := make([]string, len(names))
		for i, name := range names {
			parts[i] = name + ": " + t.Fields[name].String()
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return "any"
	}
}

// aliases maps every bare-keyword spelling a `: Ident` annotation may use to
// its canonical Type. "int"/"integer"/"float" and "boolean" are accepted
// because they're already in use today — docs/language-design.md's own
// examples use them — even though mhl has exactly one numeric runtime
// representation (float64) and one bool spelling.
var aliases = map[string]Type{
	"string":  String,
	"number":  Number,
	"int":     Number,
	"integer": Number,
	"float":   Number,
	"bool":    Bool,
	"boolean": Bool,
	"array":   Array,
	"object":  Object,
	"any":     Any,
}

// Parse resolves a bare keyword spelling to a Type — the base case FromExpr
// bottoms out at for a plain Ident with no `[]` suffix or `{ }` shape. ok is
// false for an unrecognized spelling (e.g. a typo like "sting"). name == ""
// resolves to Any, true — identical treatment to an explicit `: any`.
func Parse(name string) (Type, bool) {
	if name == "" {
		return Any, true
	}
	t, ok := aliases[name]
	return t, ok
}

// Of reports the Type that best describes v's dynamic Go representation — the
// same switch internal/engine/interpreter.typeName performs for error-text
// formatting, duplicated here (not imported — engine depends on lang, not the
// reverse) so both packages check values against one vocabulary. nil always
// reports (Any, true): null is assignable anywhere. ok is false for a Go type
// with no declarable static type in this vocabulary (e.g. a closure) — Check
// turns that into a mismatch.
//
// Of deliberately always returns the unshaped Array/Object (Elem/Fields
// nil), never attempting to infer an element type or field shape from v's
// actual contents (e.g. inferring string[] from []any{"a","b"}) — that is
// out of scope for this vocabulary's static-inference use (see
// internal/lang/lint/varinfer.go), which only ever needs "what flat Kind is
// this literal," not a full shape reconstruction.
func Of(v any) (Type, bool) {
	switch v.(type) {
	case nil:
		return Any, true
	case string:
		return String, true
	case float64:
		return Number, true
	case bool:
		return Bool, true
	case []any:
		return Array, true
	case map[string]any:
		return Object, true
	default:
		return Any, false
	}
}

// Check reports whether v satisfies declared. Any accepts everything —
// untyped/dynamic stays exactly as permissive as it is today. Every other
// Kind requires v's dynamic Go type to match exactly (no widening, no
// number-string coercion here — see Coerce for the one place raw strings get
// converted). label is folded into the error only (e.g. "input \"count\"",
// "tool \"execution\": read_file: parameter \"path\"") — callers own their
// own message prefix.
//
// When declared is a shaped Array (Elem != nil), every element of v is
// checked recursively against Elem — this covers arbitrarily nested arrays
// (string[][]) for free, since Elem is itself a full Type tree. When
// declared is a shaped Object (Fields != nil), every declared field must be
// present in v with the right type; an extra field in v that isn't declared
// is NOT an error — this is deliberate structural (not exact) subtyping,
// matching how most structural type systems treat object shapes: an agent's
// JSON response routinely carries more fields than a caller declared
// interest in, and rejecting those would make the contract needlessly
// brittle.
func Check(label string, declared Type, v any) error {
	if declared.Kind == AnyKind || v == nil {
		return nil
	}
	actual, ok := Of(v)
	if !ok || actual.Kind != declared.Kind {
		name := "unknown"
		if ok {
			name = actual.String()
		}
		return fmt.Errorf("%s must be %s, got %s", label, declared, name)
	}
	switch declared.Kind {
	case ArrayKind:
		if declared.Elem == nil {
			return nil
		}
		for i, item := range v.([]any) {
			if err := Check(fmt.Sprintf("%s[%d]", label, i), *declared.Elem, item); err != nil {
				return err
			}
		}
	case ObjectKind:
		if declared.Fields == nil {
			return nil
		}
		obj := v.(map[string]any)
		names := make([]string, 0, len(declared.Fields))
		for k := range declared.Fields {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			fieldType := declared.Fields[name]
			fv, present := obj[name]
			if !present {
				return fmt.Errorf("%s: missing field %q (must be %s)", label, name, fieldType)
			}
			if err := Check(fmt.Sprintf("%s.%s", label, name), fieldType, fv); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckType reports whether actual satisfies declared using the same
// exact-match rule Check applies to a runtime value — declared == Any (or
// actual == Any) always passes. Used where a caller has already reduced a
// binding to its static Type (lint's variable-type inference) instead of
// holding a boxed runtime value the way Check's callers do.
//
// When either side of a shaped Array/Object comparison is unshaped (Elem or
// Fields nil), CheckType stays permissive and reports no mismatch — lint's
// static inference (see internal/lang/lint/varinfer.go) never infers a
// shape from a literal's contents (see Of's doc comment), so a locally
// inferred `var tags = ["a", "b"]` is always unshaped Array; comparing it
// against a declared `tags: string[]` parameter can't be proven wrong
// statically even if the actual elements are bad — only the runtime Check
// (which has the real value) catches that case.
func CheckType(label string, declared, actual Type) error {
	if declared.Kind == AnyKind || actual.Kind == AnyKind {
		return nil
	}
	if declared.Kind != actual.Kind {
		return fmt.Errorf("%s must be %s, got %s", label, declared, actual)
	}
	switch declared.Kind {
	case ArrayKind:
		if declared.Elem == nil || actual.Elem == nil {
			return nil
		}
		return CheckType(label+"[]", *declared.Elem, *actual.Elem)
	case ObjectKind:
		if declared.Fields == nil || actual.Fields == nil {
			return nil
		}
		names := make([]string, 0, len(declared.Fields))
		for k := range declared.Fields {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			dt := declared.Fields[name]
			at, present := actual.Fields[name]
			if !present {
				return fmt.Errorf("%s: missing field %q (must be %s)", label, name, dt)
			}
			if err := CheckType(fmt.Sprintf("%s.%s", label, name), dt, at); err != nil {
				return err
			}
		}
	}
	return nil
}
