package interpreter

import (
	"fmt"
	"reflect"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// evalMatchExpr evaluates a `match subject { pattern -> body ... }`
// expression. The subject is evaluated once; arms are tried top to bottom.
// An arm matches when it is the `_` wildcard, or when its pattern evaluates
// to a value deep-equal to the subject — the same comparison `==` uses
// (reflect.DeepEqual, see evalEq), so an enum value only matches the same
// enum's same variant and never a bare string. The matching arm's body is
// evaluated and returned. No matching arm and no `_` is a runtime error;
// `mhl lint` reports the statically-provable non-exhaustive cases first.
func evalMatchExpr(ctx *evalCtx, e *ast.MatchExpr, depth int) (any, error) {
	subject, err := evalExprAt(ctx, e.Subject, depth)
	if err != nil {
		return nil, err
	}
	for _, arm := range e.Arms {
		if arm.Wildcard {
			return evalExprAt(ctx, arm.Body, depth)
		}
		pat, err := evalExprAt(ctx, arm.Pattern, depth)
		if err != nil {
			return nil, err
		}
		if reflect.DeepEqual(subject, pat) {
			return evalExprAt(ctx, arm.Body, depth)
		}
	}
	return nil, fmt.Errorf("match: no arm matched value %s", formatValue(subject))
}
