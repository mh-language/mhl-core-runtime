package interpreter

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/engine/value"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// enumValue is a runtime value of a declared `enum` (value.Enum — see its
// doc comment). Produced by qualified access (`Status.Draft`, see
// evalPostfix), a `match` arm's pattern, `Status.parse(text)`, and an
// enum-typed input (execsvc.coerceInputs).
type enumValue = value.Enum

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
	return enumValue{Enum: resolveName(prog, name), Variant: variant}, true, nil
}

// evalEnumMethod handles the two calls a declared enum answers:
//
//   - `Status.parse(text)` — the variant named text, or an error listing
//     every variant. For text from outside the type system (a JSON file, a
//     tool argument); an enum-typed pipeline input is already converted on
//     the way in (runtime.ConvertEnums). A value already of this enum is
//     returned unchanged.
//   - `Status.values()` — every variant, in declaration order.
//
// handled is false when name is not an enum or member is neither method.
func evalEnumMethod(ctx *evalCtx, name, member string, call *ast.Call, depth int) (any, bool, error) {
	if member != "parse" && member != "values" {
		return nil, false, nil
	}
	e, ok := findEnum(ctx.prog, name)
	if !ok {
		return nil, false, nil
	}
	canonical := resolveName(ctx.prog, name)
	if member == "values" {
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("%s.values() takes no arguments", name)
		}
		out := make([]any, len(e.Variants))
		for i, v := range e.Variants {
			out[i] = enumValue{Enum: canonical, Variant: v}
		}
		return out, true, nil
	}
	args, err := evalPositionalValues(ctx, call, depth)
	if err != nil {
		return nil, true, err
	}
	if len(args) != 1 {
		return nil, true, fmt.Errorf("%s.parse(text) takes exactly one argument", name)
	}
	if ev, ok := args[0].(enumValue); ok && ev.Enum == canonical {
		return ev, true, nil
	}
	text, ok := args[0].(string)
	if !ok {
		return nil, true, fmt.Errorf("%s.parse(text) requires a string, got %s", name, typeName(args[0]))
	}
	if !enumHasVariant(e, text) {
		return nil, true, fmt.Errorf("%q is not a variant of enum %s (use %s)", text, name, strings.Join(e.Variants, " | "))
	}
	return enumValue{Enum: canonical, Variant: text}, true, nil
}
