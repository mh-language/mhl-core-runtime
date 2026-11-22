package runtime

import (
	"fmt"
	"strings"
)

// BreakSignal is what a StepFunc returns (via cli.go's exec closure, which
// translates interpreter.IsBreak's result into this) when the step it just
// ran executed an explicit `break`. Run treats it as a clean early exit, not
// a step failure: it is not wrapped as a "step failed" error, the run ends
// with exit 0, and the variable state built up so far is returned as the
// run's result (RunResult.FinalVars) exactly as on a normal completion — so
// `break` keeps its output rather than discarding it. The one difference
// from a normal completion is that the pipeline's checkpoint is left in
// place rather than cleared. Reason carries whatever value (if any) the
// break statement's optional expression evaluated to.
//
// runtime deliberately defines its own copy of this concept rather than
// importing interpreter.breakSignal — the package stays independent of the
// interpreter the same way it already is everywhere else (see this
// package's doc comment); cli.go is the one place both are in scope to
// translate between them.
type BreakSignal struct{ Reason any }

func (b *BreakSignal) Error() string { return "break" }

// GotoSignal is the same kind of translated signal for a `goto Target`
// statement. Run resumes execution at Target instead of the step that would
// otherwise come next in declaration order.
type GotoSignal struct{ Target string }

func (g *GotoSignal) Error() string { return "goto " + g.Target }

// PauseSignal is the translated signal for a `pause(...)` builtin call. Run
// suspends the pipeline at the pausing step — it is neither a failure nor a
// completion: RunResult.Paused is set, the step's checkpoint is written (so a
// later --resume / run/resume re-enters that same step), and the run's
// variable state so far is returned as FinalVars, exactly as `break` now does.
// Reason carries whatever value (if any) the pause argument evaluated to, for
// the caller / human operator to see in the run status. This is the primitive
// for a human-in-the-loop delegation.
type PauseSignal struct {
	Reason any
	// Gate is set when the pause is a typed one, `pause(reason, decision: T)`:
	// the run waits for a decision of type T (see Gate).
	Gate *Gate
	// Inputs is set when the runner itself suspends the run on entering a
	// named stage whose required inputs are unbound (see InputGate).
	Inputs *InputGate
}

// InputGate identifies a run suspended on entering named stage Stage — at
// unit Step, its entry or a goto target inside it — because the stage's
// required inputs Inputs (in declaration order) are not bound yet. Unlike a
// Gate it carries no decision type: a resume answers it with inputs
// (`--resume --input name=value`, run/resume {arguments}), checked by
// Pipeline.ValidateStageInputs. Persisted as Checkpoint.InputGate.
type InputGate struct {
	Step   string   `json:"step"`
	Stage  string   `json:"stage"`
	Inputs []string `json:"inputs"`
}

// reason is the pause reason an InputGate reports (RunResult.PauseReason).
func (g *InputGate) reason() string {
	return fmt.Sprintf("awaiting inputs for stage %q: %s", g.Stage, strings.Join(g.Inputs, ", "))
}

// Gate identifies a typed pause — `pause(reason, decision: T)` — a run is
// suspended at: the Index-th gated pause evaluated in Step (0-based, in
// evaluation order, counted afresh each time the step runs), waiting for a
// decision of type Type (as written). It is persisted in the checkpoint so
// a resume knows which gate a decision answers, and what it must be.
type Gate struct {
	Step  string `json:"step"`
	Index int    `json:"index"`
	Type  string `json:"type"`
	// Name and Doc are what the program calls the decision and says it
	// means: the name the gate's result is bound to and its `///` doc
	// (ast.GateDoc), for a client to label the decision form with.
	Name string `json:"name,omitempty"`
	Doc  string `json:"doc,omitempty"`
	// Answered are the decisions the step's earlier gates (Index 0 to
	// Index-1) returned in the execution that paused here. A resume re-enters
	// the step from its start, so it replays them: each earlier gate returns
	// its answer again instead of pausing, and the run reaches this gate.
	Answered []GateReply `json:"answered,omitempty"`
}

// GateReply is one earlier gate's answer (Gate.Answered): its decision
// type and the decision, as given.
type GateReply struct {
	Type     string `json:"type"`
	Decision any    `json:"decision"`
}

func (p *PauseSignal) Error() string { return "pause" }

// CompleteSignal is the translated signal for a `complete()` builtin call.
// Run treats it exactly like walking off the end of the merged step list —
// the run ends in the normal "completed" state, checkpoint cleared, right
// at this step, regardless of what (if anything) is declared after it. It
// exists so a convergence/terminal step never has to rely on being
// physically last: in a `partial` pipeline especially, where the merged
// order depends on cross-file `import` order rather than one file's own
// layout, that reliance is exactly the footgun `complete()` removes — see
// ast.Pipeline.Partial's doc comment.
type CompleteSignal struct{}

func (c *CompleteSignal) Error() string { return "complete" }
