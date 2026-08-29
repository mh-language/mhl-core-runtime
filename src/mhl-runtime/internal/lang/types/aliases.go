package types

import (
	"fmt"
	"sort"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// AliasError is one problem found while resolving a program's `type X = ...`
// declarations. Line/Column locate the offending `type` declaration so a
// caller (lint) can turn it straight into a Finding, and the interpreter can
// surface it as a positioned error.
type AliasError struct {
	Name    string
	Line    int
	Column  int
	Message string
}

func (e AliasError) Error() string { return e.Message }

// Aliases resolves every `type X = <TypeExpr>` declaration in prog into a
// map from alias name to the Type its target expression denotes, so a later
// FromExprAlias call can resolve a bare `: X` annotation. Aliases may refer
// to other aliases; resolution is an iterative fixpoint. Each of the
// following is reported as an AliasError (and the offending name is left out
// of the returned map): a duplicate `type` name, a name that shadows a
// builtin type keyword, a target that references an unknown type, and a
// cycle (`type A = B` / `type B = A`).
//
// prog is expected to be the fully merged program (after import resolution),
// so an alias declared in a `use`d module resolves the same way it does at
// run time.
func Aliases(prog *ast.Program) (map[string]Type, []AliasError) {
	resolved := map[string]Type{}
	if prog == nil {
		return resolved, nil
	}

	pending := map[string]*ast.TypeAlias{}
	order := make([]string, 0)
	var errs []AliasError

	// An `enum` name is a resolvable type on its own (`: Status`). Register
	// each first so a `type` alias may target one, and so a duplicate-name
	// clash between an enum and an alias is caught.
	for _, decl := range prog.Decls {
		if decl.Enum == nil {
			continue
		}
		e := decl.Enum
		if _, isBuiltin := aliases[e.Name]; isBuiltin {
			errs = append(errs, AliasError{e.Name, e.Pos.Line, e.Pos.Column,
				fmt.Sprintf("enum %q shadows a builtin type keyword", e.Name)})
			continue
		}
		if _, dup := resolved[e.Name]; dup {
			errs = append(errs, AliasError{e.Name, e.Pos.Line, e.Pos.Column,
				fmt.Sprintf("enum %q: a type with this name is already declared", e.Name)})
			continue
		}
		resolved[e.Name] = EnumType(e.Name)
	}

	for _, decl := range prog.Decls {
		if decl.Type == nil {
			continue
		}
		a := decl.Type
		if _, isBuiltin := aliases[a.Name]; isBuiltin {
			errs = append(errs, AliasError{a.Name, a.Pos.Line, a.Pos.Column,
				fmt.Sprintf("type alias %q shadows a builtin type keyword", a.Name)})
			continue
		}
		if _, isEnum := resolved[a.Name]; isEnum {
			errs = append(errs, AliasError{a.Name, a.Pos.Line, a.Pos.Column,
				fmt.Sprintf("type alias %q: an enum with this name is already declared", a.Name)})
			continue
		}
		if _, dup := pending[a.Name]; dup {
			errs = append(errs, AliasError{a.Name, a.Pos.Line, a.Pos.Column,
				fmt.Sprintf("type alias %q is already declared", a.Name)})
			continue
		}
		pending[a.Name] = a
		order = append(order, a.Name)
	}

	// Iterative fixpoint: at most one alias resolves per pass in the worst
	// case (a linear chain), so len(order) passes always reach the fixpoint.
	for pass := 0; pass <= len(order); pass++ {
		progressed := false
		for _, name := range order {
			if _, done := resolved[name]; done {
				continue
			}
			if t, ok := FromExprAlias(pending[name].Type, resolved); ok {
				resolved[name] = t
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	for _, name := range order {
		if _, done := resolved[name]; done {
			continue
		}
		a := pending[name]
		msg := fmt.Sprintf("type alias %q: unrecognized type %q", name, a.Type.String())
		for _, ref := range typeExprRefs(a.Type) {
			if _, isPending := pending[ref]; isPending {
				msg = fmt.Sprintf("type alias %q: cyclic type reference through %q", name, ref)
				break
			}
		}
		errs = append(errs, AliasError{name, a.Pos.Line, a.Pos.Column, msg})
	}

	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Line != errs[j].Line {
			return errs[i].Line < errs[j].Line
		}
		return errs[i].Column < errs[j].Column
	})
	return resolved, errs
}

// typeExprRefs returns the bare Ident names a type expression depends on
// (the leaf keyword/alias names, recursively through `[]` suffixes and
// inline `{ }` shape fields). Builtin keywords are included too; callers
// that only care about alias refs filter with the alias set they hold.
func typeExprRefs(e *ast.TypeExpr) []string {
	if e == nil {
		return nil
	}
	if e.Shape != nil {
		var refs []string
		for _, f := range e.Shape.Fields {
			refs = append(refs, typeExprRefs(f.Type)...)
		}
		return refs
	}
	if e.Name != "" {
		return []string{e.Name}
	}
	return nil
}
