package runtime

// BreakSignal is what a StepFunc returns (via cli.go's exec closure, which
// translates interpreter.IsBreak's result into this) when the step it just
// ran executed an explicit `break`. Run treats it as a soft stop, not a
// step failure: it does not wrap it as a "step failed" error, and it does
// not clear the pipeline's checkpoint the way a normal completion does.
// Reason carries whatever value (if any) the break statement's optional
// expression evaluated to.
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
