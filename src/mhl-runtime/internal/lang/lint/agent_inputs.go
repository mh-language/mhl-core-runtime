package lint

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkAgentInputs validates an agent's `input name: T [= default]`
// declarations, mirroring interpreter.bindAgentInputs: an input may not take
// a run argument's name (prompt/schema/skills/timeout — run would read it as
// that argument), may not be declared twice, must have a resolvable type, and
// a literal default must fit it. Because run forwards the inputs a call
// passes to every fallback leg, each fallback must declare every input of
// this agent, and may not require one a call is allowed to omit.
func checkAgentInputs(file string, prog *ast.Program, aliases map[string]types.Type, name string, a *ast.Agent) []Finding {
	var findings []Finding
	add := func(pos lexer.Position, format string, args ...any) {
		findings = append(findings, Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column,
			Message: fmt.Sprintf("agent %q: ", name) + fmt.Sprintf(format, args...)})
	}
	seen := map[string]bool{}
	for _, in := range a.Inputs {
		switch {
		case ast.IsAgentRunArg(in.Name):
			add(in.Pos, "input %q clashes with run's own %q argument — pick another name", in.Name, in.Name)
		case seen[in.Name]:
			add(in.Pos, "input %q is declared twice", in.Name)
		}
		seen[in.Name] = true
		t, ok := types.FromExprAlias(in.Type, aliases)
		if !ok {
			add(in.Pos, "input %q has an unrecognized type %q", in.Name, in.Type)
			continue
		}
		if in.Default != nil {
			if v, literal := ast.LiteralValue(in.Default); literal {
				if err := types.Check(fmt.Sprintf("default for input %q", in.Name), t, v); err != nil {
					add(in.Pos, "%s", err)
				}
			}
		}
	}

	refs, err := ast.AgentFallbackRefs(a)
	if err != nil {
		return findings // reported by checkAgentCall's own fallback check
	}
	for i, ref := range refs {
		fb, fbName := ref.Inline, fmt.Sprintf("%s.fallback[%d]", name, i)
		if fb == nil {
			var found bool
			if fb, found = findAgent(prog, ref.Name); !found {
				continue
			}
			fbName = ref.Name
		}
		for _, in := range a.Inputs {
			if ast.IsAgentRunArg(in.Name) {
				continue // already reported as a clash above
			}
			if _, ok := fb.FindInput(in.Name); !ok {
				add(a.Pos, "fallback %q does not declare input %q — run forwards every input a call passes to each fallback", fbName, in.Name)
			}
		}
		for _, fin := range fb.Inputs {
			if fin.Default != nil {
				continue
			}
			if pin, ok := a.FindInput(fin.Name); !ok || pin.Default != nil {
				add(a.Pos, "fallback %q requires input %q, which a call to %q may omit — give it a default, or require it here too", fbName, fin.Name, name)
			}
		}
	}
	return findings
}

// runArgProblems reports every problem the runtime would reject in the
// arguments of an `Agent.run(...)` call — or a router's `.delegate(...)`,
// which forwards its call to whichever of its agents is chosen: a positional
// argument, each name that is neither a run argument nor an input every one
// of agents declares, and each required input (no default) of any of them
// that the call leaves out. All of them, not just the first, so a call with a
// misspelled input — which is then also missing — shows both at once. callee
// names the call ("Coder.run") for the messages. A literal value given to an
// input (`repo: 42`) is also checked against the input's declared type, the
// way bindAgentInputs checks it at run time; a computed value is left to the
// run. Mirrors
// interpreter.checkRunArgs/bindAgentInputs, so a `model: "x"` passed by
// mistake fails mhl lint instead of running with the declared model.
func runArgProblems(callee string, call *ast.Call, aliases map[string]types.Type, agents ...*ast.Agent) []string {
	common := commonInputs(agents)
	accepts := ast.AgentRunArgList()
	if len(common) > 0 {
		accepts += " and the inputs " + strings.Join(common, ", ")
	}
	var problems []string
	passed := map[string]bool{}
	positional := false
	for _, arg := range call.Args {
		if arg.Name == "" {
			if !positional {
				problems = append(problems, fmt.Sprintf("%s takes named arguments only (%s)", callee, accepts))
			}
			positional = true
			continue
		}
		passed[arg.Name] = true
		if ast.IsAgentRunArg(arg.Name) {
			continue
		}
		if contains(common, arg.Name) {
			problems = append(problems, inputValueProblems(callee, arg, aliases, agents)...)
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: unknown argument %q — run accepts %s", callee, arg.Name, accepts))
	}
	missing := map[string]bool{}
	for _, a := range agents {
		for _, in := range a.Inputs {
			if in.Default == nil && !passed[in.Name] && !missing[in.Name] {
				missing[in.Name] = true
				problems = append(problems, fmt.Sprintf("%s: missing required input %q", callee, in.Name))
			}
		}
	}
	return problems
}

// inputValueProblems checks a literal argument value against the type each
// of agents declares for that input (once per distinct message).
func inputValueProblems(callee string, arg *ast.Argument, aliases map[string]types.Type, agents []*ast.Agent) []string {
	v, literal := ast.LiteralValue(arg.Value)
	if !literal {
		return nil
	}
	var problems []string
	seen := map[string]bool{}
	for _, a := range agents {
		in, ok := a.FindInput(arg.Name)
		if !ok {
			continue
		}
		t, ok := types.FromExprAlias(in.Type, aliases)
		if !ok {
			continue // the declaration's own finding (checkAgentInputs)
		}
		if err := types.Check(fmt.Sprintf("%s: input %q", callee, arg.Name), t, v); err != nil && !seen[err.Error()] {
			seen[err.Error()] = true
			problems = append(problems, err.Error())
		}
	}
	return problems
}

// commonInputs is the input names every one of agents declares, in the first
// agent's declaration order — what a call reaching any of them may pass.
func commonInputs(agents []*ast.Agent) []string {
	if len(agents) == 0 {
		return nil
	}
	var names []string
	for _, in := range agents[0].Inputs {
		all := true
		for _, a := range agents[1:] {
			if _, ok := a.FindInput(in.Name); !ok {
				all = false
				break
			}
		}
		if all {
			names = append(names, in.Name)
		}
	}
	return names
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// routerAgentDecls resolves a router's `agents: [...]` to the declared agents
// lint can find (an unresolved name is checkRouterAgentRefs' finding).
func routerAgentDecls(prog *ast.Program, router *ast.Router) []*ast.Agent {
	names, err := ast.RouterAgentRefs(router)
	if err != nil {
		return nil
	}
	var agents []*ast.Agent
	for _, n := range names {
		if a, ok := findAgent(prog, n); ok {
			agents = append(agents, a)
		}
	}
	return agents
}
