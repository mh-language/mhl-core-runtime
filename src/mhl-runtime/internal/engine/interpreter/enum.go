package interpreter

import (
	"encoding/json"
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// enumValue is a runtime value of a declared `enum` — a variant name tagged
// with its enum's name. It is deliberately distinct from a plain string:
// `Status.Draft == "Draft"` is false, and type_of(Status.Draft) is "enum".
// Produced only by qualified access (`Status.Draft`, see evalPostfix); a
// `match` arm's pattern produces one the same way.
type enumValue struct {
	Enum    string
	Variant string
}

// EnumName satisfies types.EnumCarrier so the types package can recognise an
// enum value in Of/Check without importing this package.
func (e enumValue) EnumName() string { return e.Enum }

// String renders just the bare variant name — what interpolation and log
// show (see formatValue). The enum name is available via EnumName()/type
// checks, not this display form.
func (e enumValue) String() string { return e.Variant }

// MarshalJSON serialises an enum value as its bare variant string, so
// json.stringify(Status.Draft) is "Draft" (not the internal struct) and any
// other json.Marshal path — checkpoint persistence, structured output —
// round-trips it the same readable way. nativeops stays MHL-agnostic; the
// behaviour travels with the value.
func (e enumValue) MarshalJSON() ([]byte, error) { return json.Marshal(e.Variant) }

// findEnum resolves an enum declaration by name, honouring import aliases the
// same way findTool/findAgent do.
func findEnum(prog *ast.Program, name string) (*ast.Enum, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Enum != nil && decl.Enum.Name == name {
			return decl.Enum, true
		}
	}
	return nil, false
}

// enumHasVariant reports whether e declares the given variant name.
func enumHasVariant(e *ast.Enum, variant string) bool {
	for _, v := range e.Variants {
		if v == variant {
			return true
		}
	}
	return false
}

// resolveEnumAccess turns a `Name.Variant` postfix head into an enumValue
// when Name is a declared enum. ok is false when Name is not an enum (the
// caller then falls through to its normal identifier resolution); a real
// error is returned only when Name IS an enum but the variant is unknown.
func resolveEnumAccess(prog *ast.Program, name, variant string) (enumValue, bool, error) {
	e, isEnum := findEnum(prog, name)
	if !isEnum {
		return enumValue{}, false, nil
	}
	if !enumHasVariant(e, variant) {
		return enumValue{}, true, fmt.Errorf("enum %q has no variant %q", name, variant)
	}
	return enumValue{Enum: name, Variant: variant}, true, nil
}
