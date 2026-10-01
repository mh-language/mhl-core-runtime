package parser

import (
	"reflect"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

var (
	objectFieldType = reflect.TypeOf((*ast.ObjectField)(nil))
	argumentType    = reflect.TypeOf((*ast.Argument)(nil))
)

// expandShorthand rewrites, in place, every shorthand object field
// (`{name}`) and shorthand named argument (`f(name:)`) under root into its
// long form — the field/argument value becomes the bare identifier `name`.
// It runs on every parsed AST before anything else sees it, so the
// interpreter, lint, the LSP and DefinitionDigest only ever see the long
// form: `{artifact, path}` hashes and evaluates exactly like `{artifact:
// artifact, path: path}`.
func expandShorthand(root any) {
	walkShorthand(reflect.ValueOf(root))
}

func walkShorthand(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		switch v.Type() {
		case objectFieldType:
			f := v.Interface().(*ast.ObjectField)
			if f.Shorthand != nil {
				name := string(*f.Shorthand)
				f.KeyIdent = &name
				f.Value = identExpr(name, f.Pos)
				f.Shorthand = nil
			}
		case argumentType:
			a := v.Interface().(*ast.Argument)
			if a.Value == nil {
				a.Value = identExpr(a.Name, lexer.Position{})
			}
		}
		walkShorthand(v.Elem())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			walkShorthand(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkShorthand(v.Index(i))
		}
	}
}

// identExpr builds the expression for a bare identifier reference, at pos.
func identExpr(name string, pos lexer.Position) *ast.Expr {
	return &ast.Expr{Pos: pos, Or: &ast.OrExpr{Head: &ast.AndExpr{Head: &ast.EqExpr{Head: &ast.CmpExpr{Head: &ast.AddExpr{
		Head: &ast.MulExpr{Head: &ast.Unary{Operand: &ast.Postfix{Primary: &ast.Primary{Ident: name}}}},
	}}}}}}
}
