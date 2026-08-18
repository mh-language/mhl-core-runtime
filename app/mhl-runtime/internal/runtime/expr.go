package runtime

import "github.com/yanjustino/mhl-runtime/internal/ast"

// bareObject returns the object literal of an expression that is a single
// primary with no operators applied, or nil.
func bareObject(e *ast.Expr) *ast.Object {
	if pf := barePostfix(e); pf != nil && len(pf.Ops) == 0 && pf.Primary != nil {
		return pf.Primary.Object
	}
	return nil
}

// barePostfix unwraps an expression down to its Postfix node when no binary or
// unary operators are applied along the way; returns nil otherwise.
func barePostfix(e *ast.Expr) *ast.Postfix {
	if e == nil || e.Or == nil {
		return nil
	}
	or := e.Or
	if len(or.Tail) != 0 || or.Head == nil {
		return nil
	}
	and := or.Head
	if len(and.Tail) != 0 || and.Head == nil {
		return nil
	}
	eq := and.Head
	if len(eq.Tail) != 0 || eq.Head == nil {
		return nil
	}
	cmp := eq.Head
	if len(cmp.Tail) != 0 || cmp.Head == nil {
		return nil
	}
	add := cmp.Head
	if len(add.Tail) != 0 || add.Head == nil {
		return nil
	}
	mul := add.Head
	if len(mul.Tail) != 0 || mul.Head == nil {
		return nil
	}
	u := mul.Head
	if u == nil || u.Op != "" || u.Operand == nil {
		return nil
	}
	return u.Operand
}
