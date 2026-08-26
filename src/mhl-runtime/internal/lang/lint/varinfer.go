package lint

import (
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
	"github.com/yanjustino/mhl-runtime/internal/lang/types"
)

// bareAssignName reports name, true only when target is a bare identifier
// with NO trailers at all (unlike assignTargetBase, which also accepts
// arr[i]/matrix[i][j] index chains as valid assignment targets). Used
// specifically to gate type-merging: assigning to arr[0] mutates an
// element, not arr's own type, and must never touch known["arr"].
func bareAssignName(p *ast.Postfix) (string, bool) {
	if p == nil || p.Primary == nil || p.Primary.Ident == "" || len(p.Ops) != 0 {
		return "", false
	}
	return p.Primary.Ident, true
}

// inferExprType infers expr's static type against known's CURRENT state
// (enables same-pass chaining: `var b = a` sees whatever known["a"] already
// holds). prog/selfTool resolve a `target.method(...)` call against a
// declared Tool. Returns types.Any for any shape this v1 doesn't understand
// (arithmetic, member/index access, an untyped-return call, an agent/memory
// call, ...) — deliberately narrow, see varinfer's callers for why.
func inferExprType(prog *ast.Program, expr *ast.Expr, known map[string]types.Type, selfTool *ast.Tool) types.Type {
	if lv, err := literalValue(expr); err == nil {
		if t, ok := types.Of(lv); ok {
			return t
		}
		return types.Any
	}
	if name, ok := ast.IdentValue(expr); ok {
		if t, ok := known[name]; ok {
			return t
		}
		return types.Any
	}
	if _, target, method, ok := methodCall(expr); ok {
		var tool *ast.Tool
		if target == "self" {
			tool = selfTool
		} else {
			tool, _ = findTool(prog, target)
		}
		if tool != nil {
			for _, m := range tool.Methods {
				if m.Name == method && m.Returns != nil {
					if t, ok := types.FromExpr(m.Returns); ok {
						return t
					}
				}
			}
		}
	}
	return types.Any
}

// mergeVarType infers value's type and folds it into known[name]: first
// sighting sets it outright; a later sighting that disagrees (or that
// resolves to Any) downgrades known[name] to Any and never upgrades it back
// — the monotonic, single-pass, non-flow-sensitive rule variable-type
// inference rests on. An incorrectly-confident static type is worse than
// giving up, so any conflict or uncertainty always wins toward Any.
func mergeVarType(known map[string]types.Type, prog *ast.Program, name string, value *ast.Expr, selfTool *ast.Tool) {
	inferred := inferExprType(prog, value, known, selfTool)
	if existing, ok := known[name]; !ok || existing.Equal(inferred) {
		known[name] = inferred
		return
	}
	known[name] = types.Any
}
