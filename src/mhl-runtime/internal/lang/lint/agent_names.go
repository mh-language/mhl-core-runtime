package lint

import (
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkAgentExprNames reports a name an agent's own expressions read that
// resolves to nothing — `cwd: reepo` for `input repo` — which otherwise only
// fails as `undefined variable "reepo"` the first time the agent is called.
//
// The scope is what the agent is meant to read: its declared inputs, the
// `context` accessor, declared/imported names (tools, agents, enums, ...),
// builtins and native namespaces, plus a hook's own parameters (`result` in
// `after`). Unlike the receiver-only checkUndefinedNames, a bare identifier
// is checked too (undefWalker.strict): an agent body has no local
// declarations to over-approximate, so that is precise here.
//
// Only an agent that declares inputs is checked. Its expressions are still
// evaluated in the calling scope at run time, so reading a caller variable
// works — and for an agent without inputs it is the documented way to vary
// a call (`log: "run.${run_tag}.log"`), left alone. An agent with inputs has
// a declared per-call surface instead, and lint cannot tell one call site's
// variables from another's, so a name outside it is reported.
//
// String literals the runtime uses raw are skipped: `command`, `stdin`,
// `engine`, `description` and the literal elements of `args` (their
// "${...}" spans are checkAgentPlaceholders'); `log` and `cwd` literals are
// interpolated, so their spans are checked.
func checkAgentExprNames(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	u := &undefWalker{file: file, prog: prog, strict: true}
	seen := map[*ast.Agent]bool{}
	var check func(a *ast.Agent)
	check = func(a *ast.Agent) {
		if a == nil || seen[a] {
			return
		}
		seen[a] = true
		if len(a.Inputs) == 0 {
			// Without inputs, reading the caller's variables is the only
			// way to vary a call — `log: "run.${run_tag}.log"` is
			// documented to resolve against the calling scope — so an
			// agent that declares none is left exactly as before.
			return
		}
		scope := map[string]types.Type{"context": types.Object}
		for _, in := range a.Inputs {
			t, ok := types.FromExprAlias(in.Type, aliases)
			if !ok {
				t = types.Any
			}
			scope[in.Name] = t
		}
		for _, in := range a.Inputs {
			u.expr(in.Default, scope)
		}
		for _, p := range a.Props {
			u.agentProp(p, scope)
		}
		if refs, err := ast.AgentFallbackRefs(a); err == nil {
			for _, r := range refs {
				check(r.Inline)
			}
		}
	}
	for _, decl := range prog.Decls {
		if decl.Agent != nil {
			check(decl.Agent)
		}
		if decl.Router != nil {
			if ref, ok, err := ast.RouterDeciderRef(decl.Router); ok && err == nil {
				check(ref.Inline)
			}
		}
	}
	return u.findings
}

// agentProp walks one agent property's value in scope (see
// checkAgentExprNames for what is skipped and why).
func (u *undefWalker) agentProp(p *ast.Property, scope map[string]types.Type) {
	if _, literal := ast.StringValue(p.Value); literal && p.Name != "log" && p.Name != "cwd" {
		return
	}
	switch p.Name {
	case "args":
		if arr := ast.BareArray(p.Value); arr != nil {
			for _, item := range arr.Items {
				if _, literal := ast.StringValue(item); !literal {
					u.expr(item, scope)
				}
			}
			return
		}
	case "before", "after", "system_prompt":
		if l, ok := ast.LambdaValue(p.Value); ok {
			hookScope := scope
			if p.Name == "after" {
				hookScope = copyScope(scope)
				hookScope["result"] = types.String
			}
			u.lambda(l, hookScope)
			return
		}
	case "fallback":
		// Bare names are declared agents (checkAgentCall reports a missing
		// one); inline agents are walked on their own by checkAgentExprNames.
		return
	}
	u.expr(p.Value, scope)
}
