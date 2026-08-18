package skills

import "github.com/yanjustino/mhl-runtime/internal/ast"

// stringValue extracts the literal string value of a simple string/multiline
// expression, if that is what the expression is.
func stringValue(e *ast.Expr) (string, bool) {
	p := primaryOf(e)
	if p == nil {
		return "", false
	}
	switch {
	case p.Str != nil:
		return *p.Str, true
	case p.MultiStr != nil:
		return *p.MultiStr, true
	}
	return "", false
}

// refListValue flattens an array-literal of tool/mcp_server references (e.g.
// `[execution.read_file, execution.git_diff]` or `[PostgresDB]`) into their
// dotted-name string form. Entries that are not simple references are skipped.
func refListValue(e *ast.Expr) []string {
	p := primaryOf(e)
	if p == nil || p.Array == nil {
		return nil
	}
	var out []string
	for _, item := range p.Array.Items {
		if name, ok := refName(item); ok {
			out = append(out, name)
		}
	}
	return out
}

// refName renders a simple dotted reference expression (an identifier followed
// only by `.member` accesses, e.g. `execution.read_file`) into its string
// form. It returns false for anything more complex.
func refName(e *ast.Expr) (string, bool) {
	pf := postfixOf(e)
	if pf == nil || pf.Primary == nil {
		return "", false
	}
	if pf.Primary.Ident == "" {
		return "", false
	}
	name := pf.Primary.Ident
	for _, t := range pf.Ops {
		if t.Member == "" {
			// A call or other trailer means this is not a plain reference.
			return "", false
		}
		name += "." + t.Member
	}
	return name, true
}

// primaryOf unwraps an expression down to its single Primary node when the
// expression is a bare primary (no operators applied). Returns nil otherwise.
func primaryOf(e *ast.Expr) *ast.Primary {
	pf := postfixOf(e)
	if pf == nil || len(pf.Ops) != 0 {
		return nil
	}
	return pf.Primary
}

// postfixOf unwraps an expression down to its Postfix node when no binary or
// unary operators are applied along the way. Returns nil otherwise.
func postfixOf(e *ast.Expr) *ast.Postfix {
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
	unary := mul.Head
	if unary == nil || unary.Op != "" {
		return nil
	}
	return unary.Operand
}
