package types

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// FromExpr resolves a parsed *ast.TypeExpr into this package's Type
// vocabulary, recursively for array-element suffixes and inline object
// field shapes. nil e (an absent, optional annotation — Param.Type or
// ToolMethod.Returns with no ": Type" at all) resolves to Any, true — the
// identical treatment Parse("") already gives an absent annotation. ok is
// false the moment any part of the tree contains an unrecognized bare
// keyword (a typo like ": sting", ": sting[]", or "{ age: sting }") or a
// shape with a field name declared more than once — the caller (lint: a
// Finding; the interpreter: an error) reports it exactly as an unresolved
// Parse already does, rendering e.String() (see ast.TypeExpr.String) to
// quote back the surface syntax the user actually wrote.
//
// FromExpr does not know about `type X = ...` aliases; a call site that has
// a program (and therefore an alias table from Aliases) must use
// FromExprAlias instead.
func FromExpr(e *ast.TypeExpr) (Type, bool) {
	return FromExprAlias(e, nil)
}

// FromExprAlias is FromExpr with a resolved alias table consulted before the
// bare-keyword base case: a plain Ident that names a `type X = ...` alias
// (or an `enum`) resolves to that alias's Type. A nil aliases map makes it
// behave exactly like FromExpr.
func FromExprAlias(e *ast.TypeExpr, aliases map[string]Type) (Type, bool) {
	if e == nil {
		return Any, true
	}
	if n := len(e.ArraySuffixes); n > 0 {
		inner := *e
		inner.ArraySuffixes = e.ArraySuffixes[:n-1]
		elem, ok := FromExprAlias(&inner, aliases)
		if !ok {
			return Any, false
		}
		return ArrayOf(elem), true
	}
	if e.Shape != nil {
		fields := make(map[string]Type, len(e.Shape.Fields))
		for _, f := range e.Shape.Fields {
			if _, dup := fields[f.Name]; dup {
				return Any, false // duplicate field name in the same shape
			}
			ft, ok := FromExprAlias(f.Type, aliases)
			if !ok {
				return Any, false
			}
			fields[f.Name] = ft
		}
		return ObjectOf(fields), true
	}
	if t, ok := aliases[e.Name]; ok {
		return t, true
	}
	return Parse(e.Name)
}
