package runtime

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
type PauseSignal struct{ Reason any }

func (p *PauseSignal) Error() string { return "pause" }
