package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkLoopStopWhen statically flags a `loop pipeline`'s `stop_when` when it
// references one of that pipeline's own `var` declarations. It mirrors a gap
// in interpreter.EvalCondition (internal/engine/interpreter/condition.go):
// stop_when is evaluated against a fresh, empty environment that never sees
// pipeline-level vars, even though those same vars are valid read/write
// targets inside every step of that pipeline (see checkAgentCalls's
// pipelineVars handling above). Referencing one in stop_when therefore
// always fails at run time with a generic "undefined variable" error, only
// surfacing once the loop finishes its first iteration (stop_when is
// re-checked after every full iteration — runtime.LoopRunner.Run,
// internal/engine/runtime/loop.go). Independently, a loop pipeline's vars
// are also re-initialized fresh every iteration (interpreter.EvalPipelineVars
// is called anew each time runtime.Runner.Run executes the pipeline), so
// even a stop_when that could see them would never observe a value
// accumulated *across* iterations. Either way the fix is the same: `memory`
// (e.g. a kv memory's session.get/session.set) is what's meant to carry
// state a stop_when needs to read, both within and across iterations.
func checkLoopStopWhen(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil || !decl.Pipeline.Loop {
			continue
		}
		pipelineVars := collectPipelineVarNames(prog, decl.Pipeline)
		if len(pipelineVars) == 0 {
			continue
		}
		for _, member := range decl.Pipeline.Body {
			if member.Prop == nil || member.Prop.Name != "repeat" {
				continue
			}
			stopWhen := repeatStopWhenExpr(member.Prop.Value)
			if stopWhen == nil {
				continue
			}
			seen := map[string]bool{}
			walkExprIdents(stopWhen, func(name string) {
				if _, ok := pipelineVars[name]; !ok || seen[name] {
					return
				}
				seen[name] = true
				findings = append(findings, Finding{
					File: file, Line: member.Prop.Pos.Line, Column: member.Prop.Pos.Column,
					Message: fmt.Sprintf("loop pipeline %q: stop_when references var %q, but stop_when never sees pipeline vars and they reset every iteration anyway — use memory (e.g. a kv memory's get/set) for state stop_when needs to read", decl.Pipeline.Name, name),
				})
			})
		}
	}
	return findings
}

// repeatStopWhenExpr reads just the stop_when field out of a `repeat { ... }`
// property's object literal — the lint-side twin of
// runtime.repeatConfigFromExpr (internal/engine/runtime/pipeline.go), kept
// separate rather than shared since internal/lang may not depend on
// internal/engine (see this repo's README on the one-way internal/
// dependency order).
func repeatStopWhenExpr(e *ast.Expr) *ast.Expr {
	obj := ast.BareObject(e)
	if obj == nil {
		return nil
	}
	for _, f := range obj.Fields {
		key := ""
		switch {
		case f.KeyIdent != nil:
			key = *f.KeyIdent
		case f.KeyStr != nil:
			key = *f.KeyStr
		}
		if key == "stop_when" {
			return f.Value
		}
	}
	return nil
}

// walkExprIdents calls fn once for every bare identifier reachable from
// expr — the base of a Postfix chain (`count` in `count == 10`, `agent` in
// `agent.run(...)`, `arr` and `i` in `arr[i]`), including those nested in
// call arguments, index/slice bounds, and object/array literal values. It
// does not distinguish a variable read from a call/member target — for
// checkLoopStopWhen's purpose (does this expression mention this pipeline
// var's name at all) that distinction doesn't matter.
func walkExprIdents(expr *ast.Expr, fn func(name string)) {
	if expr == nil {
		return
	}
	walkOr(expr.Or, fn)
	for _, op := range expr.Tail {
		walkOr(op.Rhs, fn)
	}
}

func walkOr(e *ast.OrExpr, fn func(string)) {
	if e == nil {
		return
	}
	walkAnd(e.Head, fn)
	for _, op := range e.Tail {
		walkAnd(op.Rhs, fn)
	}
}

func walkAnd(e *ast.AndExpr, fn func(string)) {
	if e == nil {
		return
	}
	walkEq(e.Head, fn)
	for _, op := range e.Tail {
		walkEq(op.Rhs, fn)
	}
}

func walkEq(e *ast.EqExpr, fn func(string)) {
	if e == nil {
		return
	}
	walkCmp(e.Head, fn)
	for _, op := range e.Tail {
		walkCmp(op.Rhs, fn)
	}
}

func walkCmp(e *ast.CmpExpr, fn func(string)) {
	if e == nil {
		return
	}
	walkAdd(e.Head, fn)
	for _, op := range e.Tail {
		walkAdd(op.Rhs, fn)
	}
}

func walkAdd(e *ast.AddExpr, fn func(string)) {
	if e == nil {
		return
	}
	walkMul(e.Head, fn)
	for _, op := range e.Tail {
		walkMul(op.Rhs, fn)
	}
}

func walkMul(e *ast.MulExpr, fn func(string)) {
	if e == nil {
		return
	}
	walkUnary(e.Head, fn)
	for _, op := range e.Tail {
		walkUnary(op.Rhs, fn)
	}
}

func walkUnary(e *ast.Unary, fn func(string)) {
	if e == nil {
		return
	}
	walkPostfix(e.Operand, fn)
}

func walkPostfix(p *ast.Postfix, fn func(string)) {
	if p == nil {
		return
	}
	walkPrimary(p.Primary, fn)
	for _, op := range p.Ops {
		switch {
		case op.Call != nil:
			for _, arg := range op.Call.Args {
				walkExprIdents(arg.Value, fn)
			}
		case op.Slice != nil:
			if op.Slice.Low != nil {
				walkExprIdents(op.Slice.Low.Value, fn)
			}
			if op.Slice.High != nil {
				walkExprIdents(op.Slice.High.Value, fn)
			}
		case op.Index != nil:
			walkExprIdents(op.Index, fn)
		}
	}
}

func walkPrimary(p *ast.Primary, fn func(string)) {
	if p == nil {
		return
	}
	switch {
	case p.Ident != "":
		fn(p.Ident)
	case p.Object != nil:
		for _, f := range p.Object.Fields {
			walkExprIdents(f.Value, fn)
		}
	case p.Array != nil:
		for _, item := range p.Array.Items {
			walkExprIdents(item, fn)
		}
	case p.IfExpr != nil:
		walkExprIdents(p.IfExpr.Cond, fn)
		walkExprIdents(p.IfExpr.Then, fn)
		walkExprIdents(p.IfExpr.Else, fn)
	case p.Lambda != nil:
		walkExprIdents(p.Lambda.Body, fn)
		// Lambda.Block is a statement list, not an *ast.Expr this walker
		// reaches — stop_when has no realistic use for one.
	case p.Sub != nil:
		walkExprIdents(p.Sub, fn)
	}
}
