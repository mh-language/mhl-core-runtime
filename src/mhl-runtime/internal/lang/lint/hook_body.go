package lint

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// hookBuiltinNames is every bare identifier a pipeline hook body can
// reference, as a zero-trailer plain value, without it being a declared
// name, a pipeline input/var/mem, or a name declared inside the hook body
// itself — mirrors the interpreter's own special-cased dispatch for a
// zero-trailer identifier (evalPostfixOps's top of internal/engine/
// interpreter/eval.go: log/fail/pause/complete/env/nameof, self, context,
// and the type_of/is_* builtins, typeBuiltinWants) by hand, since lint and
// the interpreter are different packages with no shared symbol table —
// update both together if that dispatch ever changes.
var hookBuiltinNames = map[string]bool{
	"log": true, "fail": true, "pause": true, "complete": true,
	"env": true, "nameof": true, "self": true, "context": true,
	"type_of": true, "is_string": true, "is_number": true, "is_bool": true,
	"is_array": true, "is_object": true, "is_null": true, "is_enum": true,
}

// checkPipelineHookBody flags a bare identifier used as a plain value — no
// member, call, or index trailer at all, e.g. `eventType` in `{ x:
// eventType }`, not `eventType.foo` or `eventType()` — inside a pipeline/
// workflow lifecycle hook body that resolves to nothing: not the hook's own
// parameter, not a pipeline input/var/mem, not a name declared inside the
// hook body itself (a `var`, `catch (e)`, a for-loop element, ...), not a
// top-level declared name (the shape `nameof`'s target also has), and not
// one of hookBuiltinNames.
//
// Deliberately narrow: an identifier that IS the target of a member access
// or call (`Writer.generate(...)`, `self.method()`, `Billing.run(...)`) is
// left unchecked — it may legitimately be an agent/tool/router/memory name
// dispatched by evalPostfixOps's own trailer-aware special-casing, a much
// larger surface this check does not attempt to reproduce (that dispatch is
// itself the "different classes of chain, resolved differently" logic
// several *other*, older lint checks already specialize in narrowly, one
// shape at a time — checkAgentCalls's `.run(...)`, checkRouterDelegateCallShape's
// `.delegate(...)`, and so on — rather than something this check should
// duplicate). A nested lambda's own body is not recursed into either, for
// the same reason: its parameter introduces its own local scope this check
// does not attempt to track. Both restrictions favor never reporting a
// false positive over catching every possible case.
//
// This is what caught a real bug: workflows/discovery/discovery.partial.hook.mh's
// `step_end` hook referenced an undefined `eventType` as a plain
// object-literal field value — silently swallowed by the hook's own
// try/catch, so `mhl lint` reported "No problems found" and the bug (this
// project's telemetry never actually sent) went unnoticed.
//
// Skipped entirely for a `partial` pipeline not yet at EntryStepCount()==1
// — the same reasoning checkAgentCalls/checkPipelineGoto already apply:
// this fragment may legitimately reference a name declared in a sibling
// fragment this lint pass never saw (discovery.partial.hook.mh's own
// self.artifact/self.project_id, read via the OTHER hook property in that
// same fragment, are exactly this case).
func checkPipelineHookBody(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		if decl.Pipeline.Partial && decl.Pipeline.EntryStepCount() != 1 {
			continue
		}
		pipelineInputs := pipelineInputTypes(decl.Pipeline, aliases)
		pipelineVars := collectPipelineVarNames(prog, decl.Pipeline)
		pipelineMemVars := collectPipelineMemNames(prog, decl.Pipeline)
		for _, member := range decl.Pipeline.Body {
			if member.Prop == nil || !isHookPropertyName(member.Prop.Name) {
				continue
			}
			lambda, ok := ast.LambdaValue(member.Prop.Value)
			if !ok {
				continue // hook shape (must be a 1-param lambda) is validated elsewhere
			}
			seed := make(map[string]types.Type, len(pipelineInputs)+len(pipelineVars)+len(pipelineMemVars)+2)
			for name, t := range pipelineInputs {
				seed[name] = t
			}
			for name, t := range pipelineVars {
				seed[name] = t
			}
			for name, t := range pipelineMemVars {
				seed[name] = t
			}
			seed["context"] = types.Object
			if len(lambda.Params) == 1 {
				seed[lambda.Params[0].Name] = types.Any
			}
			if lambda.Block != nil {
				declared := collectVarNames(prog, lambda.Block, seed, nil)
				findings = append(findings, checkStmtsIdents(file, prog, lambda.Block, declared)...)
				continue
			}
			if lambda.Body != nil {
				findings = append(findings, checkExprIdents(file, prog, lambda.Body, seed)...)
			}
		}
	}
	return findings
}

// isHookPropertyName reports whether name is one of the five pipeline
// lifecycle hooks (the ones with a ParamType in ast.PipelineBodyProperties)
// rather than a plain config property like checkpoint/output.
func isHookPropertyName(name string) bool {
	for _, p := range ast.PipelineBodyProperties {
		if p.Name == name {
			return p.ParamType != ""
		}
	}
	return false
}

// isHookIdentResolvable reports whether a zero-trailer bare identifier
// named name is something checkPipelineHookBody accepts: a name in
// declared (the hook's param, a pipeline input/var/mem, or a name declared
// inside the hook body itself), a fixed builtin, or any top-level declared
// name in prog (nameof's target position uses this exact shape).
func isHookIdentResolvable(prog *ast.Program, name string, declared map[string]types.Type) bool {
	if _, ok := declared[name]; ok {
		return true
	}
	if hookBuiltinNames[name] {
		return true
	}
	return declaredNameExists(prog, name)
}

func checkStmtsIdents(file string, prog *ast.Program, stmts []*ast.Statement, declared map[string]types.Type) []Finding {
	var findings []Finding
	for _, s := range stmts {
		findings = append(findings, checkStmtIdents(file, prog, s, declared)...)
	}
	return findings
}

func checkStmtIdents(file string, prog *ast.Program, s *ast.Statement, declared map[string]types.Type) []Finding {
	switch {
	case s.Var != nil:
		return checkExprIdents(file, prog, s.Var.Value, declared)
	case s.Const != nil:
		return checkExprIdents(file, prog, s.Const.Value, declared)
	case s.Return != nil:
		return checkExprIdents(file, prog, s.Return.Value, declared)
	case s.Break != nil:
		return checkExprIdents(file, prog, s.Break.Reason, declared)
	case s.Assign != nil:
		return checkExprIdents(file, prog, s.Assign.Value, declared)
	case s.Expr != nil:
		return checkExprIdents(file, prog, s.Expr.Expr, declared)
	case s.If != nil:
		out := checkExprIdents(file, prog, s.If.Cond, declared)
		out = append(out, checkStmtsIdents(file, prog, s.If.Then, declared)...)
		out = append(out, checkStmtsIdents(file, prog, s.If.Else, declared)...)
		return out
	case s.While != nil:
		out := checkExprIdents(file, prog, s.While.Cond, declared)
		out = append(out, checkStmtsIdents(file, prog, s.While.Body, declared)...)
		return out
	case s.ForIn != nil:
		out := checkExprIdents(file, prog, s.ForIn.Iterable, declared)
		out = append(out, checkStmtsIdents(file, prog, s.ForIn.Body, declared)...)
		return out
	case s.Try != nil:
		out := checkStmtsIdents(file, prog, s.Try.Body, declared)
		out = append(out, checkStmtsIdents(file, prog, s.Try.Catch, declared)...)
		out = append(out, checkStmtsIdents(file, prog, s.Try.Finally, declared)...)
		return out
	}
	// Spawn, GotoMatch, Goto, and any other statement kind: left unchecked —
	// same "can't prove it, don't fail" stance the rest of this file takes,
	// and none of them are shapes the real bug motivating this check used.
	return nil
}

// checkExprIdents is the entry point for one *ast.Expr: pos (e's own
// position) is threaded down through every precedence level below, since
// only Expr itself — not OrExpr/AndExpr/.../Unary — carries a lexer.Position,
// and is refreshed automatically on the next call to checkExprIdents (every
// recursion point below eventually re-enters here with a new sub-expression
// that has its own Pos).
func checkExprIdents(file string, prog *ast.Program, e *ast.Expr, declared map[string]types.Type) []Finding {
	if e == nil {
		return nil
	}
	pos := e.Pos
	findings := checkOrIdents(file, prog, e.Or, declared, pos)
	for _, c := range e.Tail {
		findings = append(findings, checkOrIdents(file, prog, c.Rhs, declared, pos)...)
	}
	return findings
}

func checkOrIdents(file string, prog *ast.Program, o *ast.OrExpr, declared map[string]types.Type, pos lexer.Position) []Finding {
	if o == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, checkAndIdents(file, prog, o.Head, declared, pos)...)
	for _, op := range o.Tail {
		findings = append(findings, checkAndIdents(file, prog, op.Rhs, declared, pos)...)
	}
	return findings
}

func checkAndIdents(file string, prog *ast.Program, a *ast.AndExpr, declared map[string]types.Type, pos lexer.Position) []Finding {
	if a == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, checkEqIdents(file, prog, a.Head, declared, pos)...)
	for _, op := range a.Tail {
		findings = append(findings, checkEqIdents(file, prog, op.Rhs, declared, pos)...)
	}
	return findings
}

func checkEqIdents(file string, prog *ast.Program, eq *ast.EqExpr, declared map[string]types.Type, pos lexer.Position) []Finding {
	if eq == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, checkCmpIdents(file, prog, eq.Head, declared, pos)...)
	for _, op := range eq.Tail {
		findings = append(findings, checkCmpIdents(file, prog, op.Rhs, declared, pos)...)
	}
	return findings
}

func checkCmpIdents(file string, prog *ast.Program, c *ast.CmpExpr, declared map[string]types.Type, pos lexer.Position) []Finding {
	if c == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, checkAddIdents(file, prog, c.Head, declared, pos)...)
	for _, op := range c.Tail {
		findings = append(findings, checkAddIdents(file, prog, op.Rhs, declared, pos)...)
	}
	return findings
}

func checkAddIdents(file string, prog *ast.Program, a *ast.AddExpr, declared map[string]types.Type, pos lexer.Position) []Finding {
	if a == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, checkMulIdents(file, prog, a.Head, declared, pos)...)
	for _, op := range a.Tail {
		findings = append(findings, checkMulIdents(file, prog, op.Rhs, declared, pos)...)
	}
	return findings
}

func checkMulIdents(file string, prog *ast.Program, m *ast.MulExpr, declared map[string]types.Type, pos lexer.Position) []Finding {
	if m == nil {
		return nil
	}
	var findings []Finding
	findings = append(findings, checkUnaryIdents(file, prog, m.Head, declared, pos)...)
	for _, op := range m.Tail {
		findings = append(findings, checkUnaryIdents(file, prog, op.Rhs, declared, pos)...)
	}
	return findings
}

func checkUnaryIdents(file string, prog *ast.Program, u *ast.Unary, declared map[string]types.Type, pos lexer.Position) []Finding {
	if u == nil {
		return nil
	}
	return checkPostfixIdents(file, prog, u.Operand, declared, pos)
}

func checkPostfixIdents(file string, prog *ast.Program, p *ast.Postfix, declared map[string]types.Type, pos lexer.Position) []Finding {
	if p == nil || p.Primary == nil {
		return nil
	}
	var findings []Finding
	if p.Primary.Ident != "" && len(p.Ops) == 0 {
		if !isHookIdentResolvable(prog, p.Primary.Ident, declared) {
			findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
				Message: fmt.Sprintf("undefined identifier %q", p.Primary.Ident)})
		}
	}
	findings = append(findings, checkPrimaryIdents(file, prog, p.Primary, declared)...)
	for _, op := range p.Ops {
		if op.Call != nil {
			for _, arg := range op.Call.Args {
				findings = append(findings, checkExprIdents(file, prog, arg.Value, declared)...)
			}
		}
		findings = append(findings, checkExprIdents(file, prog, op.Index, declared)...)
		findings = append(findings, checkExprIdents(file, prog, op.OptIndex, declared)...)
		if op.Slice != nil {
			if op.Slice.Low != nil {
				findings = append(findings, checkExprIdents(file, prog, op.Slice.Low.Value, declared)...)
			}
			if op.Slice.High != nil {
				findings = append(findings, checkExprIdents(file, prog, op.Slice.High.Value, declared)...)
			}
		}
	}
	for _, op := range p.WithTail {
		if op.Object != nil {
			for _, f := range op.Object.Fields {
				findings = append(findings, checkExprIdents(file, prog, f.Value, declared)...)
			}
		}
	}
	return findings
}

// checkPrimaryIdents recurses into a primary's own sub-expressions —
// object/array literal values, a parenthesised sub-expression, an if/match
// branch — each re-entering checkExprIdents with its own Expr, which
// refreshes the reported position from that sub-expression's own Pos. A
// nested Lambda's body and a `ref { ... }` literal are deliberately NOT
// recursed into: see checkPipelineHookBody's doc comment.
func checkPrimaryIdents(file string, prog *ast.Program, p *ast.Primary, declared map[string]types.Type) []Finding {
	var findings []Finding
	switch {
	case p.Object != nil:
		for _, f := range p.Object.Fields {
			findings = append(findings, checkExprIdents(file, prog, f.Value, declared)...)
		}
	case p.Array != nil:
		for _, item := range p.Array.Items {
			findings = append(findings, checkExprIdents(file, prog, item, declared)...)
		}
	case p.Sub != nil:
		findings = append(findings, checkExprIdents(file, prog, p.Sub, declared)...)
	case p.IfExpr != nil:
		findings = append(findings, checkExprIdents(file, prog, p.IfExpr.Cond, declared)...)
		findings = append(findings, checkExprIdents(file, prog, p.IfExpr.Then, declared)...)
		findings = append(findings, checkExprIdents(file, prog, p.IfExpr.Else, declared)...)
	case p.Match != nil:
		findings = append(findings, checkExprIdents(file, prog, p.Match.Subject, declared)...)
		for _, arm := range p.Match.Arms {
			findings = append(findings, checkExprIdents(file, prog, arm.Pattern, declared)...)
			findings = append(findings, checkExprIdents(file, prog, arm.Body, declared)...)
		}
	}
	return findings
}
