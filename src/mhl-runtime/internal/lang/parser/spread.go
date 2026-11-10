package parser

import (
	"reflect"

	"github.com/alecthomas/participle/v2"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkSpreadPlacement rejects a `...` spread anywhere but directly as an
// element of an array literal. The grammar puts the optional `...` on
// ast.Expr itself (adding it as an Array-only wrapper type would change the
// DefinitionDigest of every program that uses an array), so it parses in any
// expression position — `f(...xs)`, `var y = ...xs`, `{ k: ...xs }` — and
// this pass is what makes all of those a parse error, caught by mhl lint and
// mhl run alike, instead of a value silently meaning something else.
//
// It walks the whole tree by reflection rather than through a hand-written
// ladder over every node type, so a node added to the grammar later is
// covered without anyone having to remember this check exists.
func checkSpreadPlacement(root any) error {
	return walkSpread(reflect.ValueOf(root), false)
}

var (
	exprType  = reflect.TypeOf(ast.Expr{})
	arrayType = reflect.TypeOf(ast.Array{})
)

// walkSpread visits v; allowed is true only for the direct items of an
// ast.Array.
func walkSpread(v reflect.Value, allowed bool) error {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return walkSpread(v.Elem(), allowed)
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := walkSpread(v.Index(i), allowed); err != nil {
				return err
			}
		}
	case reflect.Struct:
		t := v.Type()
		if t == exprType {
			e := v.Addr().Interface().(*ast.Expr)
			if e.Spread && !allowed {
				return participle.Errorf(e.Pos, "spread (...) is only allowed as an element of an array literal, e.g. [\"a\", ...items]")
			}
		}
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			itemsOfArray := t == arrayType && t.Field(i).Name == "Items"
			if err := walkSpread(v.Field(i), itemsOfArray); err != nil {
				return err
			}
		}
	}
	return nil
}
