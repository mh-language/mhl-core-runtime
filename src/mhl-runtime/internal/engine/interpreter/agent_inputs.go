package interpreter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkRunArgs rejects an `Agent.run(...)` argument the runtime doesn't read:
// a positional one, or a name that is neither a run argument
// (ast.IsAgentRunArg) nor one of agent's declared inputs. Such a name used to
// be accepted and silently ignored — a call passing `model: "x"` ran with
// the declared model — so it now fails the call. Mirrored statically by
// lint's unknownRunArg.
func checkRunArgs(agentName string, agent *ast.Agent, call *ast.Call) error {
	for _, in := range agent.Inputs {
		if ast.IsAgentRunArg(in.Name) {
			return fmt.Errorf("agent %q: input %q clashes with run's own %q argument — pick another name", agentName, in.Name, in.Name)
		}
	}
	for _, arg := range call.Args {
		if arg.Name == "" {
			return fmt.Errorf("%s.run takes named arguments only (%s)", agentName, ast.AgentRunAccepts(agent))
		}
		if ast.IsAgentRunArg(arg.Name) {
			continue
		}
		if _, ok := agent.FindInput(arg.Name); ok {
			continue
		}
		return fmt.Errorf("%s.run: unknown argument %q — run accepts %s", agentName, arg.Name, ast.AgentRunAccepts(agent))
	}
	return nil
}

// callInputArgs evaluates, in the caller's scope and once per `.run(...)`
// call, every argument that fills an input rather than a run argument
// (prompt/schema/skills/timeout). The same values are handed to the primary
// agent and to each fallback leg (bindAgentInputs). nil when there are none.
func callInputArgs(ctx *evalCtx, agentName string, call *ast.Call, depth int) (map[string]any, error) {
	var passed map[string]any
	for _, arg := range call.Args {
		if arg.Name == "" || ast.IsAgentRunArg(arg.Name) {
			continue
		}
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return nil, fmt.Errorf("%s.run: input %q: %w", agentName, arg.Name, err)
		}
		if passed == nil {
			passed = map[string]any{}
		}
		passed[arg.Name] = v
	}
	return passed, nil
}

// bindAgentInputs returns the scope agent's own expressions are evaluated
// in for one call: ctx with each declared input bound — the value the call
// passed, else the input's default (evaluated left to right in that same
// scope, so a later default may read an earlier input), else a "missing
// required input" error — and type-checked against its declaration like a
// tool method parameter. An input shadows a caller variable of the same
// name. An agent without inputs, called without input arguments, gets ctx
// itself back, so nothing about it changes.
//
// passed may name an input agent doesn't declare only for a fallback leg
// (the primary was already held to its own inputs by checkRunArgs): run
// forwards the call's inputs to every fallback, so a fallback that can't
// receive one is a declaration error — mhl lint reports it at the agent.
func bindAgentInputs(ctx *evalCtx, agentName string, agent *ast.Agent, passed map[string]any, depth int) (*evalCtx, error) {
	for _, name := range sortedKeys(passed) {
		if _, ok := agent.FindInput(name); !ok {
			return nil, fmt.Errorf("agent %q does not declare input %q, which this call passes (run forwards every input to a fallback, so each fallback must declare them)", agentName, name)
		}
	}
	if len(agent.Inputs) == 0 {
		return ctx, nil
	}
	child := *ctx
	child.env = make(Env, len(ctx.env)+len(agent.Inputs))
	for k, v := range ctx.env {
		child.env[k] = v
	}
	// An input shadows a caller const of the same name. The caller's map is
	// shared — with concurrent `spawn`ed calls, too — so it is only read;
	// when an input does shadow one, the child gets its own copy without it.
	for _, in := range agent.Inputs {
		if ctx.constNames[in.Name] {
			child.constNames = make(map[string]bool, len(ctx.constNames))
			for k := range ctx.constNames {
				child.constNames[k] = true
			}
			for _, shadow := range agent.Inputs {
				delete(child.constNames, shadow.Name)
			}
			break
		}
	}
	for _, in := range agent.Inputs {
		v, ok := passed[in.Name]
		if !ok {
			if in.Default == nil {
				return nil, fmt.Errorf("%s.run: missing required input %q", agentName, in.Name)
			}
			dv, err := evalExprAt(&child, in.Default, depth)
			if err != nil {
				return nil, fmt.Errorf("%s.run: default for input %q: %w", agentName, in.Name, err)
			}
			v = dv
		}
		declared, ok := types.FromExprAlias(in.Type, ctx.aliasTypes)
		if !ok {
			return nil, fmt.Errorf("agent %q: input %q has an unrecognized type %q", agentName, in.Name, in.Type)
		}
		if err := types.Check(fmt.Sprintf("%s.run: input %q", agentName, in.Name), declared, v); err != nil {
			return nil, err
		}
		child.env[in.Name] = v
	}
	return &child, nil
}

// substituteInputPlaceholders fills the "${name}" spans of a literal `args`
// element or `stdin:` string with agent's declared inputs, the way
// "${prompt}"/"${schema}" are filled. Those literals are otherwise used raw —
// never interpolated against the caller's scope — so only a declared input's
// own name is recognized; any other "${...}" text passes through exactly as
// before. ctx is the scope bindAgentInputs built.
func substituteInputPlaceholders(ctx *evalCtx, agent *ast.Agent, s string) string {
	if ctx == nil || len(agent.Inputs) == 0 || !strings.Contains(s, "${") {
		return s
	}
	for _, in := range agent.Inputs {
		placeholder := "${" + in.Name + "}"
		if strings.Contains(s, placeholder) {
			s = strings.ReplaceAll(s, placeholder, formatValue(ctx.env[in.Name]))
		}
	}
	return s
}

// withBindings is ctx with extra names bound on top of its env (a `before`
// hook's returned fields).
func withBindings(ctx *evalCtx, bindings map[string]any) *evalCtx {
	if len(bindings) == 0 {
		return ctx
	}
	env := make(Env, len(ctx.env)+len(bindings))
	for k, v := range ctx.env {
		env[k] = v
	}
	for k, v := range bindings {
		env[k] = v
	}
	child := *ctx
	child.env = env
	return &child
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
