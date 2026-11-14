package flow

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// GotoRef is one control transfer a step body can make: a plain `goto X`,
// one arm of a `goto match`, or one arm of a `route` reached through
// `goto Route(...)`.
type GotoRef struct {
	// Stmt is the `goto` / `goto match` statement itself.
	Stmt *ast.Statement
	// Arm is the match arm (of the `goto match`, or of Route) the transfer
	// goes through; nil for a plain `goto X`.
	Arm *ast.GotoMatchArm
	// Route is the route a `goto Route(...)` dispatched through; nil otherwise.
	Route *ast.WorkflowRoute
	// Target is the destination step, "" when Arm is a `fail(...)` arm.
	Target string
	// Guard is the innermost enclosing `if` statement, nil when the goto is
	// not under one; Else reports whether it sits in that if's else branch.
	Guard *ast.Statement
	Else  bool
}

// WalkGotos visits every control transfer in stmts in source order,
// recursing into if/while/for/try bodies. routes maps a workflow's route
// names to their declarations so `goto Route(...)` expands to one GotoRef
// per arm.
func WalkGotos(stmts []*ast.Statement, routes map[string]*ast.WorkflowRoute, visit func(GotoRef)) {
	walkGotos(stmts, routes, nil, false, visit)
}

func walkGotos(stmts []*ast.Statement, routes map[string]*ast.WorkflowRoute, guard *ast.Statement, inElse bool, visit func(GotoRef)) {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch {
		case s.Goto != nil:
			if route := routes[s.Goto.Target]; s.Goto.Called && route != nil {
				for _, arm := range route.Arms {
					visit(GotoRef{Stmt: s, Arm: arm, Route: route, Target: arm.Target, Guard: guard, Else: inElse})
				}
			} else {
				visit(GotoRef{Stmt: s, Target: s.Goto.Target, Guard: guard, Else: inElse})
			}
		case s.GotoMatch != nil:
			for _, arm := range s.GotoMatch.Arms {
				visit(GotoRef{Stmt: s, Arm: arm, Target: arm.Target, Guard: guard, Else: inElse})
			}
		case s.If != nil:
			walkGotos(s.If.Then, routes, s, false, visit)
			walkGotos(s.If.Else, routes, s, true, visit)
		case s.While != nil:
			walkGotos(s.While.Body, routes, guard, inElse, visit)
		case s.ForIn != nil:
			walkGotos(s.ForIn.Body, routes, guard, inElse, visit)
		case s.Try != nil:
			walkGotos(s.Try.Body, routes, guard, inElse, visit)
			walkGotos(s.Try.Catch, routes, guard, inElse, visit)
			walkGotos(s.Try.Finally, routes, guard, inElse, visit)
		}
	}
}

// GotoTargets returns every step stmts can jump to, in source order — a
// `fail(...)` arm contributes nothing. It is what `mhl run --dry-run`'s goto
// listing (execsvc.Inspect) reports.
func GotoTargets(stmts []*ast.Statement, routes map[string]*ast.WorkflowRoute) []string {
	var out []string
	WalkGotos(stmts, routes, func(g GotoRef) {
		if g.Target != "" {
			out = append(out, g.Target)
		}
	})
	return out
}

// Routes indexes a pipeline's `route` declarations by name.
func Routes(p *ast.Pipeline) map[string]*ast.WorkflowRoute {
	routes := map[string]*ast.WorkflowRoute{}
	for _, m := range p.Body {
		if m.Route != nil {
			routes[m.Route.Name] = m.Route
		}
	}
	return routes
}
