package interpreter

import (
	"context"
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// A gate is a typed pause: `var d = pause(reason, decision: T)`. Reached the
// first time, it suspends the run like a plain pause, recording which gate
// it is (runtime.Gate: the step, the gate's ordinal among the step's gated
// pauses, and T). A resume that supplies a decision (execsvc.Request.Decision,
// `mhl run --resume --decision`, MCP `run/resume {decision}`) answers exactly
// that gate: re-entering the step, the same call returns the decision
// instead of pausing. The answer is consumed there — the next time any gate
// is reached it pauses again — so a decision can never answer a later pause
// it was not given for. A plain `pause(reason)` is untouched by all of this.

// GateAnswer is the decision a resumed run hands to the one gate it is
// waiting at: the Index-th gated pause evaluated in Step.
type GateAnswer struct {
	Step  string
	Index int
	Value any
	// Replay holds the answers of the step's earlier gates (Index 0 to
	// Index-1) in the execution that paused, checked against their types
	// again: re-entering the step, each returns its own instead of pausing.
	Replay []GateReplay
	// Used is set once the gate has returned Value.
	Used bool
}

// GateReplay is one earlier gate's answer, replayed on re-entry.
type GateReplay struct {
	Type  string
	Value any
}

type gateAnswerKey struct{}

// WithGateAnswer returns ctx carrying a for the steps run under it. RunStep
// picks it up when it runs a.Step.
func WithGateAnswer(ctx context.Context, a *GateAnswer) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gateAnswerKey{}, a)
}

func gateAnswerFrom(ctx context.Context, step string) *GateAnswer {
	if ctx == nil {
		return nil
	}
	a, _ := ctx.Value(gateAnswerKey{}).(*GateAnswer)
	if a == nil || a.Step != step {
		return nil
	}
	return a
}

// gateState counts one step execution's gated pauses, so each can be told
// apart (Gate.Index), and holds the answer meant for one of them.
type gateState struct {
	step   string
	next   int
	answer *GateAnswer
	// answered are this execution's gate answers so far, in order — carried
	// on a later gate's pause (runtime.Gate.Answered) to be replayed.
	answered []runtime.GateReply
}

// PauseGate reports the gate a pause signal (as returned by RunStep) came
// from — nil for a plain `pause(reason)`.
func PauseGate(err error) *runtime.Gate {
	if sig := pauseSignalOf(err); sig != nil {
		return sig.gate
	}
	return nil
}

// decisionArg splits pause's arguments into the reason ones and the
// `decision:` type name ("" without one).
func decisionArg(args []*ast.Argument) (reason []*ast.Argument, typeName string, err error) {
	for _, a := range args {
		if a.Name == "" {
			reason = append(reason, a)
			continue
		}
		if a.Name != "decision" {
			return nil, "", fmt.Errorf("pause: unknown argument %q (pause takes a reason and an optional decision: Type)", a.Name)
		}
		name, ok := ast.IdentValue(a.Value)
		if !ok {
			return nil, "", fmt.Errorf("pause: decision: expects a type name (a declared `type` or `enum`, or string/number/bool)")
		}
		typeName = name
	}
	return reason, typeName, nil
}

// ResolveDecisionType resolves a gate's decision type name against prog's
// declared types and enums, then the builtin ones.
func ResolveDecisionType(prog *ast.Program, name string) (types.Type, bool) {
	return resolveTypeName(aliasTypesFor(prog), name)
}

func resolveTypeName(aliases map[string]types.Type, name string) (types.Type, bool) {
	if t, ok := aliases[name]; ok {
		return t, true
	}
	if name == "" {
		return types.Any, false
	}
	return types.Parse(name)
}

// evalGate is pause(reason..., decision: T): the answer, when the resumed
// run carries one for this gate, else a pause signal naming the gate.
func evalGate(ctx *evalCtx, reasonArgs []*ast.Argument, typeName string, doc *ast.GateDoc, depth int) (any, error) {
	if _, ok := resolveTypeName(ctx.aliasTypes, typeName); !ok {
		return nil, fmt.Errorf("pause: decision: unknown type %q", typeName)
	}
	g := ctx.gates
	if g == nil {
		return nil, fmt.Errorf("pause(..., decision: %s) is only meaningful inside a workflow step", typeName)
	}
	index := g.next
	g.next++
	if a := g.answer; a != nil && !a.Used {
		switch {
		case index < a.Index && index < len(a.Replay):
			// An earlier gate of this step, answered before the pause the
			// run is resuming from: it returns that answer again.
			r := a.Replay[index]
			g.answered = append(g.answered, runtime.GateReply{Type: r.Type, Decision: r.Value})
			return r.Value, nil
		case index == a.Index:
			a.Used = true
			g.answered = append(g.answered, runtime.GateReply{Type: typeName, Decision: a.Value})
			return a.Value, nil
		}
	}
	sig := &pauseSignal{gate: &runtime.Gate{Step: g.step, Index: index, Type: typeName, Answered: append([]runtime.GateReply(nil), g.answered...)}}
	if doc != nil {
		sig.gate.Name, sig.gate.Doc = doc.Name, doc.Doc
	}
	switch values, err := evalLogArgs(ctx, reasonArgs, depth); {
	case err != nil:
		return nil, err
	case len(values) == 1:
		sig.reason = values[0]
	case len(values) > 1:
		sig.reason = joinValues(values)
	}
	return nil, sig
}
