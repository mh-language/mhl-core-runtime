package flow

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// AlwaysDiverts reports whether executing stmts is guaranteed to transfer
// control away rather than run off the end — recognizing only what this
// codebase's real step bodies actually use: a trailing `goto`/`goto match`/
// `break`, a trailing bare `fail(...)`/`pause(...)` call, or a trailing
// `if` whose Then *and* Else branches both (recursively) always divert.
// Nothing else is recognized (a `try`/`catch`, a loop, a call whose return
// value is merely conventional) — a step relying on one of those alone to
// divert should add an explicit trailing `goto`/`fail(...)` reinforcing the
// point, which every step in this codebase already does. Only the last
// statement matters: whatever precedes it, if the list's last statement
// always diverts, the list as a whole always diverts.
//
// Used by lint's mergePartialGroup to flag the one shape of fallthrough that's
// actually dangerous — see that function's doc comment for why "a step
// with no `goto` isn't the last one" is a completely normal, common
// pattern *within* a single file (mhl's whole execution model defaults to
// sequential fallthrough; `workflow` only *adds* `goto` as an option, it
// doesn't require using it everywhere) and is only checked at the specific
// point a `partial` pipeline's fragments join — where physical order stops
// being something one file's source visibly shows.
func AlwaysDiverts(stmts []*ast.Statement) bool {
	if len(stmts) == 0 {
		return false
	}
	last := stmts[len(stmts)-1]
	switch {
	case last.Goto != nil:
		return true
	case last.GotoMatch != nil:
		// Every arm targets a declared step or a `fail(reason)` — and an
		// unmatched value with no `_` wildcard fails at runtime — so a
		// `goto match` always diverts one way or another regardless of
		// its arms (checkGotoMatchArms/checkPipelineGoto validate the
		// arms themselves elsewhere).
		return true
	case last.Break != nil:
		return true
	case last.Expr != nil && IsDivertingCall(last.Expr.Expr):
		return true
	case last.If != nil:
		return len(last.If.Else) > 0 && AlwaysDiverts(last.If.Then) && AlwaysDiverts(last.If.Else)
	default:
		return false
	}
}

// IsDivertingCall reports whether e is nothing but a bare `fail(...)`,
// `pause(...)`, or `complete()` call — mirrors interpreter.evalPostfix's
// own recognition of these builtins (internal/engine/interpreter/eval.go):
// a Postfix whose Primary is the bare identifier and whose only trailer is
// a Call, with no other operator applied (ast.BarePostfix returns nil
// otherwise). All three end a step's execution one way or another — fail
// as a failure, pause as a suspension, complete as a normal completion —
// so none of them can be followed by a fallthrough into whatever's next.
func IsDivertingCall(e *ast.Expr) bool {
	p := ast.BarePostfix(e)
	if p == nil || p.Primary == nil {
		return false
	}
	if len(p.Ops) != 1 || p.Ops[0].Call == nil {
		return false
	}
	switch p.Primary.Ident {
	case "fail", "pause", "complete":
		return true
	default:
		return false
	}
}
