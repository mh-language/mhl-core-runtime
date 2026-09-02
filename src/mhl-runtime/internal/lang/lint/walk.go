package lint

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// walkMatchExprs invokes fn for every ast.MatchExpr reachable from e,
// recursing through the whole expression grammar (operators, call
// arguments, index/slice bounds, object/array literal elements, lambda
// bodies and blocks, if-expression branches, and a nested match's own
// subject and arms). Separate from loop.go's walkExprIdents (which visits
// bare identifier names, not nodes) — kept in its own file with an `mx`
// helper prefix to avoid clashing with that ladder.
func walkMatchExprs(e *ast.Expr, fn func(*ast.MatchExpr)) {
	if e == nil {
		return
	}
	mxOr(e.Or, fn)
	for _, c := range e.Tail {
		if c != nil {
			mxOr(c.Rhs, fn)
		}
	}
}

func mxOr(o *ast.OrExpr, fn func(*ast.MatchExpr)) {
	if o == nil {
		return
	}
	mxAnd(o.Head, fn)
	for _, op := range o.Tail {
		mxAnd(op.Rhs, fn)
	}
}

func mxAnd(a *ast.AndExpr, fn func(*ast.MatchExpr)) {
	if a == nil {
		return
	}
	mxEq(a.Head, fn)
	for _, op := range a.Tail {
		mxEq(op.Rhs, fn)
	}
}

func mxEq(e *ast.EqExpr, fn func(*ast.MatchExpr)) {
	if e == nil {
		return
	}
	mxCmp(e.Head, fn)
	for _, op := range e.Tail {
		mxCmp(op.Rhs, fn)
	}
}

func mxCmp(c *ast.CmpExpr, fn func(*ast.MatchExpr)) {
	if c == nil {
		return
	}
	mxAdd(c.Head, fn)
	for _, op := range c.Tail {
		mxAdd(op.Rhs, fn)
	}
}

func mxAdd(a *ast.AddExpr, fn func(*ast.MatchExpr)) {
	if a == nil {
		return
	}
	mxMul(a.Head, fn)
	for _, op := range a.Tail {
		mxMul(op.Rhs, fn)
	}
}

func mxMul(m *ast.MulExpr, fn func(*ast.MatchExpr)) {
	if m == nil {
		return
	}
	mxUnary(m.Head, fn)
	for _, op := range m.Tail {
		mxUnary(op.Rhs, fn)
	}
}

func mxUnary(u *ast.Unary, fn func(*ast.MatchExpr)) {
	if u == nil || u.Operand == nil {
		return
	}
	mxPrimary(u.Operand.Primary, fn)
	for _, t := range u.Operand.Ops {
		mxTrailer(t, fn)
	}
}

func mxTrailer(t *ast.Trailer, fn func(*ast.MatchExpr)) {
	if t == nil {
		return
	}
	walkMatchExprs(t.OptIndex, fn)
	walkMatchExprs(t.Index, fn)
	if t.Call != nil {
		for _, arg := range t.Call.Args {
			walkMatchExprs(arg.Value, fn)
		}
	}
	if t.Slice != nil {
		if t.Slice.Low != nil {
			walkMatchExprs(t.Slice.Low.Value, fn)
		}
		if t.Slice.High != nil {
			walkMatchExprs(t.Slice.High.Value, fn)
		}
	}
}

func mxPrimary(p *ast.Primary, fn func(*ast.MatchExpr)) {
	if p == nil {
		return
	}
	switch {
	case p.Match != nil:
		fn(p.Match)
		walkMatchExprs(p.Match.Subject, fn)
		for _, arm := range p.Match.Arms {
			walkMatchExprs(arm.Pattern, fn)
			walkMatchExprs(arm.Body, fn)
		}
	case p.IfExpr != nil:
		walkMatchExprs(p.IfExpr.Cond, fn)
		walkMatchExprs(p.IfExpr.Then, fn)
		walkMatchExprs(p.IfExpr.Else, fn)
	case p.Lambda != nil:
		walkMatchExprs(p.Lambda.Body, fn)
		for _, s := range p.Lambda.Block {
			walkStmtMatchExprs(s, fn)
		}
	case p.Sub != nil:
		walkMatchExprs(p.Sub, fn)
	case p.Object != nil:
		for _, f := range p.Object.Fields {
			walkMatchExprs(f.Value, fn)
		}
	case p.Array != nil:
		for _, item := range p.Array.Items {
			walkMatchExprs(item, fn)
		}
	}
}

// walkStmtMatchExprs recurses one statement (and any nested statement
// bodies) invoking fn for every MatchExpr it can reach.
func walkStmtMatchExprs(s *ast.Statement, fn func(*ast.MatchExpr)) {
	if s == nil {
		return
	}
	switch {
	case s.Var != nil:
		walkMatchExprs(s.Var.Value, fn)
	case s.Const != nil:
		walkMatchExprs(s.Const.Value, fn)
	case s.Return != nil:
		walkMatchExprs(s.Return.Value, fn)
	case s.Break != nil:
		walkMatchExprs(s.Break.Reason, fn)
	case s.Assign != nil:
		walkMatchExprs(s.Assign.Value, fn)
	case s.Expr != nil:
		walkMatchExprs(s.Expr.Expr, fn)
	case s.Spawn != nil:
		walkMatchExprs(s.Spawn.Call, fn)
		walkMatchExprs(s.Spawn.Iterable, fn)
	case s.If != nil:
		walkMatchExprs(s.If.Cond, fn)
		for _, b := range s.If.Then {
			walkStmtMatchExprs(b, fn)
		}
		for _, b := range s.If.Else {
			walkStmtMatchExprs(b, fn)
		}
	case s.While != nil:
		walkMatchExprs(s.While.Cond, fn)
		for _, b := range s.While.Body {
			walkStmtMatchExprs(b, fn)
		}
	case s.ForIn != nil:
		walkMatchExprs(s.ForIn.Iterable, fn)
		for _, b := range s.ForIn.Body {
			walkStmtMatchExprs(b, fn)
		}
	case s.Try != nil:
		for _, b := range s.Try.Body {
			walkStmtMatchExprs(b, fn)
		}
		for _, b := range s.Try.Catch {
			walkStmtMatchExprs(b, fn)
		}
		for _, b := range s.Try.Finally {
			walkStmtMatchExprs(b, fn)
		}
	}
}
