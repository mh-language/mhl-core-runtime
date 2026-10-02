package parser

import (
	"fmt"
	"reflect"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

var statementSliceType = reflect.TypeOf([]*ast.Statement(nil))

// expandDestructure rewrites, in place, every destructuring statement under
// root into the plain statements it abbreviates:
//
//	var {data, revisao: note} = review
//
// becomes
//
//	var $destructure1 = review
//	var data = $destructure1.data
//	var note = $destructure1.revisao
//
// (`=` assignments, to `self.name` for the `self.{...}` form, instead of
// `var` for the other two forms). The right-hand side is evaluated once, into
// a temporary whose `$` name no program can spell, so it never collides with
// or shadows a user name. Like expandShorthand, this runs before anything
// else sees the AST: the interpreter, lint and the LSP only ever see `var`
// and assignment statements they already understand.
func expandDestructure(root any) {
	n := 0
	walkDestructure(reflect.ValueOf(root), &n)
}

func walkDestructure(v reflect.Value, n *int) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			walkDestructure(v.Elem(), n)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			f := v.Field(i)
			if f.Type() == statementSliceType && f.CanSet() {
				f.Set(reflect.ValueOf(expandStatements(f.Interface().([]*ast.Statement), n)))
			}
			walkDestructure(f, n)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkDestructure(v.Index(i), n)
		}
	}
}

func expandStatements(stmts []*ast.Statement, n *int) []*ast.Statement {
	var out []*ast.Statement
	for i, s := range stmts {
		if s == nil || s.Destructure == nil {
			if out != nil {
				out = append(out, s)
			}
			continue
		}
		if out == nil {
			out = append([]*ast.Statement{}, stmts[:i]...)
		}
		d := s.Destructure
		*n++
		tmp := fmt.Sprintf("$destructure%d", *n)
		out = append(out, &ast.Statement{Pos: s.Pos, Var: &ast.VarDecl{Name: tmp, Value: d.Value}})
		for _, e := range d.Entries {
			value := memberExpr(tmp, e.Field, s.Pos)
			if d.Var {
				out = append(out, &ast.Statement{Pos: s.Pos, Var: &ast.VarDecl{Name: e.Target(), Value: value}})
				continue
			}
			target := &ast.Postfix{Primary: &ast.Primary{Ident: e.Target()}}
			if d.Self {
				target = &ast.Postfix{Primary: &ast.Primary{Ident: "self"}, Ops: []*ast.Trailer{{Member: e.Target()}}}
			}
			out = append(out, &ast.Statement{Pos: s.Pos, Assign: &ast.AssignStmt{Target: target, Op: "=", Value: value}})
		}
	}
	if out == nil {
		return stmts
	}
	return out
}

// memberExpr builds `name.field`, at pos.
func memberExpr(name, field string, pos lexer.Position) *ast.Expr {
	e := identExpr(name, pos)
	e.Or.Head.Head.Head.Head.Head.Head.Operand.Ops = []*ast.Trailer{{Member: field}}
	return e
}
