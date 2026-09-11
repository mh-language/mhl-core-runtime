package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// evalNameofCall implements the nameof(Identifier) builtin: it takes a
// single bare identifier naming any top-level declaration — agent, router,
// memory, tool, prompt, pipeline/workflow, extension, type alias, or enum —
// and returns that name as a string, after validating the declaration
// actually exists. The argument is read directly off the AST
// (ast.IdentValue) rather than evaluated as an ordinary expression, because
// a bare declared name has no runtime value of its own to evaluate (a
// stand-alone `Billing` is "undefined variable" everywhere else in the
// language — declared constructs are resolved by name only at their own
// specific call sites, e.g. `Billing.run(...)`).
//
// This exists so a value that names a declared construct — most
// prominently a router's `select: (prompt) -> { return nameof(Billing) }`
// hook — can be written as an identifier reference instead of a free-
// floating string literal ("Billing"): a typo in nameof's argument is a
// clear, immediate error (both here and, for the common inline-lambda
// shape, statically at `mhl lint` time — see lint's checkNameofCallShape
// and checkRouterSelectBody), and an editor's "go to definition"/rename
// already follows Billing here as an ordinary identifier — no special LSP
// support needed beyond that.
func evalNameofCall(ctx *evalCtx, args []*ast.Argument, depth int) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("nameof takes exactly one argument, e.g. nameof(Billing)")
	}
	name, ok := ast.IdentValue(args[0].Value)
	if !ok {
		return nil, fmt.Errorf("nameof's argument must be a bare declared name, e.g. nameof(Billing) — not a string, variable, or expression")
	}
	if !declaredNameExists(ctx.prog, name) {
		return nil, fmt.Errorf("nameof: %q is not a declared name", name)
	}
	return name, nil
}

// declaredNameExists reports whether name (after resolving any import
// alias) names a top-level declaration of any kind in prog.
func declaredNameExists(prog *ast.Program, name string) bool {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		switch {
		case decl.Agent != nil && decl.Agent.Name == name:
			return true
		case decl.Router != nil && decl.Router.Name == name:
			return true
		case decl.Memory != nil && decl.Memory.Name == name:
			return true
		case decl.Tool != nil && decl.Tool.Name == name:
			return true
		case decl.Prompt != nil && decl.Prompt.Name == name:
			return true
		case decl.Pipeline != nil && decl.Pipeline.Name == name:
			return true
		case decl.Type != nil && decl.Type.Name == name:
			return true
		case decl.Enum != nil && decl.Enum.Name == name:
			return true
		case decl.Extensible != nil && decl.Extensible.Kind == name:
			return true
		}
		if _, extName, _, ok := ast.AsExtension(decl); ok && extName == name {
			return true
		}
	}
	return false
}
