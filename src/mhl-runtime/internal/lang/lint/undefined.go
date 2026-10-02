package lint

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// receiverBuiltinNames are the bare names the interpreter resolves without a
// declaration when they head a member trailer (`log.info`, `self.x`,
// `context.vars`, `env("X").trim()`) or appear inside a `${...}` span —
// nativeNamespaces plus evalPostfixOps's special-cased identifiers
// (internal/engine/interpreter/eval.go). Kept by hand, like
// hookBuiltinNames: lint does not import the interpreter.
var receiverBuiltinNames = map[string]bool{
	"self": true, "context": true, "env": true, "nameof": true,
	"fail": true, "pause": true, "complete": true,
	"type_of": true, "is_string": true, "is_number": true, "is_bool": true,
	"is_array": true, "is_object": true, "is_null": true, "is_enum": true,
}

// checkUndefinedNames flags two shapes the runtime only rejects when the
// offending line actually executes — often inside a try/catch or a hook,
// where the failure is swallowed and nothing ever reports it:
//
//   - a receiver that resolves to nothing: `Missing.pick()`, `Statuz.Ok` —
//     the identifier heading a member trailer is not a local, a pipeline
//     input/var/mem, a tool's own var/const, a builtin namespace, or a
//     declared/imported name;
//   - any identifier inside a `${...}` interpolation span that resolves to
//     nothing, e.g. `catch { log.error("${e}") }` with no `(e)` binding —
//     plus a span that does not parse as an expression at all.
//
// Bare calls (`foo(...)`) and bare zero-trailer identifiers outside
// interpolation are left to the existing narrower checks (assertion names,
// closures in scope and prompt render calls all share that shape). Scope is
// the same flat, step-wide over-approximation collectVarNames already uses,
// so a name declared anywhere in the enclosing body counts as declared;
// a lambda adds its own params and body vars on top of its enclosing scope.
//
// Covered bodies: pipeline steps (incl. parallel groups), pipeline lifecycle
// hooks, tool methods and test describes. An incomplete `partial` fragment is
// skipped, as in checkPipelineHookBody — its sibling fragments may declare
// the names it reads.
func checkUndefinedNames(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	u := &undefWalker{file: file, prog: prog}
	for _, decl := range prog.Decls {
		switch {
		case decl.Pipeline != nil:
			p := decl.Pipeline
			if p.Partial && p.EntryStepCount() != 1 {
				continue
			}
			seed := map[string]types.Type{"context": types.Object}
			for _, m := range []map[string]types.Type{pipelineInputTypes(p, aliases), collectPipelineVarNames(prog, p), collectPipelineMemNames(prog, p)} {
				for n, t := range m {
					seed[n] = t
				}
			}
			for _, member := range p.Body {
				for _, step := range pipelineMemberSteps(member) {
					u.stmts(step.Body, collectVarNames(prog, step.Body, copyScope(seed), nil))
				}
				if member.Prop != nil && isHookPropertyName(member.Prop.Name) {
					if lambda, ok := ast.LambdaValue(member.Prop.Value); ok {
						u.lambda(lambda, seed)
					}
				}
			}
		case decl.Tool != nil:
			toolNames := map[string]types.Type{}
			for _, member := range decl.Tool.Members {
				if member.Const != nil {
					toolNames[member.Const.Name] = types.Any
				} else if member.Var != nil {
					toolNames[member.Var.Name] = types.Any
				}
			}
			u.tool = decl.Tool
			for _, m := range decl.Tool.Methods {
				scope := toolMethodParamTypes(m, aliases)
				for n, t := range toolNames {
					if _, exists := scope[n]; !exists {
						scope[n] = t
					}
				}
				if m.Body != nil {
					u.expr(m.Body, scope)
				}
				if m.Block != nil {
					u.stmts(m.Block, collectVarNames(prog, m.Block, scope, decl.Tool))
				}
			}
			u.tool = nil
		case decl.Test != nil:
			// enumStringCompare is skipped here, like checkConstReassign: a
			// test is where an always-false comparison is asserted on
			// purpose (`is_false(s == "Published")`).
			u.inTest = true
			for _, d := range decl.Test.Describes {
				u.stmts(d.Body, collectVarNames(prog, d.Body, nil, nil))
			}
			u.inTest = false
		}
	}
	return u.findings
}

func copyScope(m map[string]types.Type) map[string]types.Type {
	out := make(map[string]types.Type, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

type undefWalker struct {
	file     string
	prog     *ast.Program
	findings []Finding
	// deferInterp > 0 while walking arguments whose interpolation the
	// runtime resolves in a scope lint cannot reproduce (isAgentDispatch).
	deferInterp int
	// inTest is set while walking a test's describe bodies.
	inTest bool
	// tool is the tool whose methods are being walked, nil elsewhere.
	tool *ast.Tool
}

// isAgentDispatch reports whether p is `Agent.run(...)` or
// `Router.delegate(...)` on a declared agent/router.
func (u *undefWalker) isAgentDispatch(p *ast.Postfix) bool {
	if p.Primary.Ident == "" || len(p.Ops) < 2 || p.Ops[1].Call == nil {
		return false
	}
	name := resolveName(u.prog, p.Primary.Ident)
	switch p.Ops[0].Member {
	case "run":
		_, ok := findAgent(u.prog, name)
		return ok
	case "delegate":
		_, ok := findRouter(u.prog, name)
		return ok
	}
	return false
}

func (u *undefWalker) resolvable(name string, scope map[string]types.Type) bool {
	if _, ok := scope[name]; ok {
		return true
	}
	return receiverBuiltinNames[name] || nativeNamespaces[name] || declaredNameExists(u.prog, name)
}

func (u *undefWalker) report(pos lexer.Position, format string, args ...any) {
	u.findings = append(u.findings, Finding{File: u.file, Line: pos.Line, Column: pos.Column, Message: fmt.Sprintf(format, args...)})
}

func (u *undefWalker) lambda(l *ast.Lambda, outer map[string]types.Type) {
	scope := copyScope(outer)
	for _, p := range l.Params {
		scope[p.Name] = types.Any
	}
	if l.Body != nil {
		u.expr(l.Body, scope)
	}
	if l.Block != nil {
		u.stmts(l.Block, collectVarNames(u.prog, l.Block, scope, nil))
	}
}

func (u *undefWalker) stmts(stmts []*ast.Statement, scope map[string]types.Type) {
	for _, s := range stmts {
		u.stmt(s, scope)
	}
}

func (u *undefWalker) stmt(s *ast.Statement, scope map[string]types.Type) {
	switch {
	case s.Var != nil:
		u.expr(s.Var.Value, scope)
	case s.Const != nil:
		u.expr(s.Const.Value, scope)
	case s.Return != nil:
		u.expr(s.Return.Value, scope)
	case s.Break != nil:
		u.expr(s.Break.Reason, scope)
	case s.Assign != nil:
		u.postfix(s.Assign.Target, scope, lexer.Position{})
		u.expr(s.Assign.Value, scope)
	case s.Expr != nil:
		u.expr(s.Expr.Expr, scope)
	case s.Spawn != nil:
		u.expr(s.Spawn.Call, scope)
		u.expr(s.Spawn.Iterable, scope)
	case s.Goto != nil:
		for _, a := range s.Goto.Args {
			u.expr(a.Value, scope)
		}
	case s.GotoMatch != nil:
		u.expr(s.GotoMatch.Subject, scope)
		for _, arm := range s.GotoMatch.Arms {
			u.expr(arm.Pattern, scope)
			u.expr(arm.Fail, scope)
		}
	case s.If != nil:
		u.expr(s.If.Cond, scope)
		u.stmts(s.If.Then, scope)
		u.stmts(s.If.Else, scope)
	case s.While != nil:
		u.expr(s.While.Cond, scope)
		u.stmts(s.While.Body, scope)
	case s.ForIn != nil:
		u.expr(s.ForIn.Iterable, scope)
		u.stmts(s.ForIn.Body, scope)
	case s.Try != nil:
		u.stmts(s.Try.Body, scope)
		u.stmts(s.Try.Catch, scope)
		u.stmts(s.Try.Finally, scope)
	}
}

func (u *undefWalker) expr(e *ast.Expr, scope map[string]types.Type) {
	if e == nil {
		return
	}
	u.or(e.Or, scope, e.Pos)
	for _, c := range e.Tail {
		u.or(c.Rhs, scope, e.Pos)
	}
}

func (u *undefWalker) or(o *ast.OrExpr, scope map[string]types.Type, pos lexer.Position) {
	if o == nil {
		return
	}
	u.and(o.Head, scope, pos)
	for _, op := range o.Tail {
		u.and(op.Rhs, scope, pos)
	}
}

func (u *undefWalker) and(a *ast.AndExpr, scope map[string]types.Type, pos lexer.Position) {
	if a == nil {
		return
	}
	u.eq(a.Head, scope, pos)
	for _, op := range a.Tail {
		u.eq(op.Rhs, scope, pos)
	}
}

func (u *undefWalker) eq(e *ast.EqExpr, scope map[string]types.Type, pos lexer.Position) {
	if e == nil {
		return
	}
	u.cmp(e.Head, scope, pos)
	left := e.Head
	for _, op := range e.Tail {
		u.enumStringCompare(left, op.Rhs, scope, pos)
		u.enumStringCompare(op.Rhs, left, scope, pos)
		u.cmp(op.Rhs, scope, pos)
		left = op.Rhs
	}
}

// enumStringCompare flags `x == "text"` / `x != "text"` where x is statically
// an enum value: an enum never equals a string, so the comparison is constant
// — almost always code written when x was still a string.
func (u *undefWalker) enumStringCompare(a, b *ast.CmpExpr, scope map[string]types.Type, pos lexer.Position) {
	if u.inTest {
		return
	}
	name, ok := cmpOperand(a)
	if !ok || name.Primary.Ident == "" || len(name.Ops) != 0 {
		return
	}
	t, ok := scope[name.Primary.Ident]
	if !ok || t.Kind != types.EnumKind {
		return
	}
	lit, ok := cmpOperand(b)
	if !ok || lit.Primary.Str == nil || len(lit.Ops) != 0 {
		return
	}
	e, ok := findEnumDecl(u.prog, resolveName(u.prog, t.Name))
	if !ok {
		return
	}
	for _, v := range e.Variants {
		if v == *lit.Primary.Str {
			u.report(pos, "%s is an enum %s and never equals the string %q — compare with %s.%s", name.Primary.Ident, t.Name, v, t.Name, v)
			return
		}
	}
	u.report(pos, "%s is an enum %s and never equals the string %q, which is not one of its variants (%s)", name.Primary.Ident, t.Name, *lit.Primary.Str, strings.Join(e.Variants, " | "))
}

// cmpOperand unwraps a comparison operand that is a single postfix
// expression with no arithmetic or unary operator around it.
func cmpOperand(c *ast.CmpExpr) (*ast.Postfix, bool) {
	if c == nil || len(c.Tail) != 0 || c.Head == nil || len(c.Head.Tail) != 0 {
		return nil, false
	}
	m := c.Head.Head
	if m == nil || len(m.Tail) != 0 || m.Head == nil || m.Head.Op != "" || m.Head.Operand == nil || m.Head.Operand.Primary == nil {
		return nil, false
	}
	return m.Head.Operand, true
}

func (u *undefWalker) cmp(c *ast.CmpExpr, scope map[string]types.Type, pos lexer.Position) {
	if c == nil {
		return
	}
	u.add(c.Head, scope, pos)
	for _, op := range c.Tail {
		u.add(op.Rhs, scope, pos)
	}
}

func (u *undefWalker) add(a *ast.AddExpr, scope map[string]types.Type, pos lexer.Position) {
	if a == nil {
		return
	}
	u.mul(a.Head, scope, pos)
	for _, op := range a.Tail {
		u.mul(op.Rhs, scope, pos)
	}
}

func (u *undefWalker) mul(m *ast.MulExpr, scope map[string]types.Type, pos lexer.Position) {
	if m == nil {
		return
	}
	if m.Head != nil {
		u.postfix(m.Head.Operand, scope, pos)
	}
	for _, op := range m.Tail {
		if op.Rhs != nil {
			u.postfix(op.Rhs.Operand, scope, pos)
		}
	}
}

func (u *undefWalker) postfix(p *ast.Postfix, scope map[string]types.Type, pos lexer.Position) {
	if p == nil || p.Primary == nil {
		return
	}
	if name := p.Primary.Ident; name != "" && len(p.Ops) > 0 && p.Ops[0].Member != "" && !hasSpecificNotFoundCheck(p) && !u.resolvable(name, scope) {
		u.report(pos, "undefined name %q (not a local, input/var/mem, builtin, or declared/imported name)", name)
	}
	u.internalCall(p, scope, pos)
	u.primary(p.Primary, scope, pos)
	for i, op := range p.Ops {
		if op.Call != nil {
			// An agent's `.run(...)` / a router's `.delegate(...)` arguments
			// are interpolated against the agent `before` hook's returned
			// fields too (interpreter.runAgent's promptCtx), which lint
			// cannot see — their `${...}` spans are not checked.
			deferred := i == 1 && u.isAgentDispatch(p)
			if deferred {
				u.deferInterp++
			}
			for _, arg := range op.Call.Args {
				u.expr(arg.Value, scope)
			}
			if deferred {
				u.deferInterp--
			}
		}
		u.expr(op.Index, scope)
		u.expr(op.OptIndex, scope)
		if op.Slice != nil {
			if op.Slice.Low != nil {
				u.expr(op.Slice.Low.Value, scope)
			}
			if op.Slice.High != nil {
				u.expr(op.Slice.High.Value, scope)
			}
		}
	}
	for _, op := range p.WithTail {
		if op.Object != nil {
			for _, f := range op.Object.Fields {
				u.expr(f.Value, scope)
			}
		}
	}
}

// hasSpecificNotFoundCheck reports whether p is a call shape an older check
// already reports with a more precise message when its receiver is unknown
// — `X.run(...)` ("agent not found"), `X.delegate/select(...)` ("router not
// found") and `X.get/set/append(...)` ("memory not found") — so the same
// name is not reported twice.
func hasSpecificNotFoundCheck(p *ast.Postfix) bool {
	if len(p.Ops) < 2 || p.Ops[1].Call == nil {
		return false
	}
	switch m := p.Ops[0].Member; m {
	case "run", "delegate", "select":
		return true
	default:
		return isMemoryMethod(m)
	}
}

func (u *undefWalker) primary(p *ast.Primary, scope map[string]types.Type, pos lexer.Position) {
	switch {
	case p.Str != nil:
		u.interpolation(*p.Str, scope, pos)
	case p.MultiStr != nil:
		u.interpolation(*p.MultiStr, scope, pos)
	case p.Object != nil:
		for _, f := range p.Object.Fields {
			u.expr(f.Value, scope)
		}
	case p.Array != nil:
		for _, item := range p.Array.Items {
			u.expr(item, scope)
		}
	case p.Sub != nil:
		u.expr(p.Sub, scope)
	case p.IfExpr != nil:
		u.expr(p.IfExpr.Cond, scope)
		u.expr(p.IfExpr.Then, scope)
		u.expr(p.IfExpr.Else, scope)
	case p.Match != nil:
		u.expr(p.Match.Subject, scope)
		for _, arm := range p.Match.Arms {
			u.expr(arm.Pattern, scope)
			u.expr(arm.Body, scope)
		}
	case p.Lambda != nil:
		u.lambda(p.Lambda, scope)
	case p.Ref != nil:
		if p.Ref.Object != nil {
			for _, f := range p.Ref.Object.Fields {
				u.expr(f.Value, scope)
			}
		}
		u.expr(p.Ref.Sub, scope)
	}
}

// interpolation re-parses every `${...}` span of a string literal the way
// interpreter.interpolate does at run time, and checks each span's
// expression — including a bare zero-trailer identifier, the `${e}` shape.
func (u *undefWalker) interpolation(s string, scope map[string]types.Type, pos lexer.Position) {
	if u.deferInterp > 0 {
		return
	}
	for i := 0; i < len(s); {
		start := strings.Index(s[i:], "${")
		if start == -1 {
			return
		}
		start += i
		end, ok := parser.MatchInterpolationSpan(s, start)
		if !ok {
			u.report(pos, "unterminated \"${\" in string")
			return
		}
		inner := s[start+2 : end]
		e, err := parser.ParseExpr(inner)
		if err != nil {
			u.report(pos, "invalid expression in \"${%s}\": %v", inner, err)
		} else {
			if pf := ast.BarePostfix(e); pf != nil && pf.Primary != nil && pf.Primary.Ident != "" && len(pf.Ops) == 0 &&
				!u.resolvable(pf.Primary.Ident, scope) {
				u.report(pos, "undefined name %q in \"${%s}\"", pf.Primary.Ident, inner)
			}
			u.expr(e, scope)
		}
		i = end + 1
	}
}

// checkPipelineHookParamTypes flags a lifecycle hook whose parameter is
// annotated with a builtin context type other than the one that hook is
// bound to (`step_end: (ctx: SessionContext) -> ...`). The runtime never
// checks the annotation, so the mismatch is silent there — but the LSP
// completes fields from it, offering ones the bound value does not have.
func checkPipelineHookParamTypes(file string, prog *ast.Program) []Finding {
	want := map[string]string{}
	for _, p := range ast.PipelineBodyProperties {
		if p.ParamType != "" {
			want[p.Name] = p.ParamType
		}
	}
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, member := range decl.Pipeline.Body {
			if member.Prop == nil || want[member.Prop.Name] == "" {
				continue
			}
			lambda, ok := ast.LambdaValue(member.Prop.Value)
			if !ok || len(lambda.Params) != 1 {
				continue
			}
			t := lambda.Params[0].Type
			if t == nil || t.Name == "" || len(t.ArraySuffixes) > 0 || t.Name == want[member.Prop.Name] {
				continue
			}
			if !isHookContextType(t.Name) {
				continue
			}
			findings = append(findings, Finding{File: file, Line: t.Pos.Line, Column: t.Pos.Column,
				Message: fmt.Sprintf("%s hook parameter is bound to a %s, not a %s", member.Prop.Name, want[member.Prop.Name], t.Name)})
		}
	}
	return findings
}

func isHookContextType(name string) bool {
	for _, p := range ast.PipelineBodyProperties {
		if p.ParamType == name {
			return true
		}
	}
	return false
}

// internalCall flags `Tool.method(...)` where method is `internal` and the
// caller is neither one of Tool's own methods nor a test in the file that
// declares Tool — mirroring interpreter.checkInternalAccess.
func (u *undefWalker) internalCall(p *ast.Postfix, scope map[string]types.Type, pos lexer.Position) {
	name := p.Primary.Ident
	if name == "" || len(p.Ops) < 2 || p.Ops[0].Member == "" || p.Ops[0].Optional || p.Ops[1].Call == nil {
		return
	}
	if _, shadowed := scope[name]; shadowed {
		return
	}
	tool, ok := findTool(u.prog, name)
	if !ok {
		return
	}
	for _, m := range tool.Methods {
		if m.Name != p.Ops[0].Member || !m.Internal {
			continue
		}
		if (u.tool != nil && u.tool.Name == tool.Name) || (u.inTest && !tool.Imported) {
			return
		}
		u.report(pos, "%s.%s is internal to tool %s — call it as self.%s from inside the tool", name, m.Name, tool.Name, m.Name)
		return
	}
}
