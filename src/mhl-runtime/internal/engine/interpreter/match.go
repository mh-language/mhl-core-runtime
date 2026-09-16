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

func execGotoMatch(ctx *evalCtx, m *ast.GotoMatchStmt) error {
	subject, err := evalExpr(ctx, m.Subject)
	if err != nil {
		return err
	}
	return execGotoArms(ctx, subject, m.Arms)
}

func execGotoArms(ctx *evalCtx, subject any, arms []*ast.GotoMatchArm) error {
	for _, arm := range arms {
		matched := arm.Wildcard
		if !matched {
			pattern, err := evalExpr(ctx, arm.Pattern)
			if err != nil {
				return err
			}
			matched = reflect.DeepEqual(subject, pattern)
		}
		if !matched {
			continue
		}
		if arm.Fail != nil {
			reason, err := evalExpr(ctx, arm.Fail)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s", formatValue(reason))
		}
		return &gotoSignal{target: arm.Target}
	}
	return fmt.Errorf("goto match: no arm matched value %s", formatValue(subject))
}

func execGotoRoute(ctx *evalCtx, call *ast.GotoStmt) error {
	route := findWorkflowRoute(ctx.prog, ctx.pipelineName, call.Target)
	if route == nil {
		return fmt.Errorf("workflow route %q not found", call.Target)
	}
	if len(call.Args) != len(route.Params) {
		return fmt.Errorf("route %s expects %d argument(s), got %d", route.Name, len(route.Params), len(call.Args))
	}
	routeCtx := *ctx
	routeCtx.env = make(Env, len(ctx.env)+len(route.Params))
	for k, v := range ctx.env {
		routeCtx.env[k] = v
	}
	for i, param := range route.Params {
		if call.Args[i].Name != "" {
			return fmt.Errorf("route %s arguments are positional", route.Name)
		}
		v, err := evalExpr(ctx, call.Args[i].Value)
		if err != nil {
			return err
		}
		routeCtx.env[param.Name] = v
	}
	if len(route.Params) != 1 {
		return fmt.Errorf("route %s must declare exactly one match parameter", route.Name)
	}
	return execGotoArms(&routeCtx, routeCtx.env[route.Params[0].Name], route.Arms)
}

func findWorkflowRoute(prog *ast.Program, pipelineName, name string) *ast.WorkflowRoute {
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil || decl.Pipeline.Name != pipelineName {
			continue
		}
		for _, member := range decl.Pipeline.Body {
			if member.Route != nil && member.Route.Name == name {
				return member.Route
			}
		}
	}
	return nil
}
