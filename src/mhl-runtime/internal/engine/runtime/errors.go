package runtime

import "fmt"

// StepError wraps the failure of one pipeline step so a caller can recover
// which step failed and why without parsing the message. Runner.Run returns
// it for a step that raised an error or blew its `timeout`; Error() is
// byte-identical to the string Run produced before this type existed, and
// Unwrap() exposes the cause (so errors.Is(err, ErrStepTimeout) and
// errors.As to the underlying interpreter error both still work).
type StepError struct {
	Pipeline string
	Step     string
	// Kind is one of a closed set: "failed" (an ordinary step error),
	// "timeout" (the step exceeded its `timeout <dur>` clause), "cancelled"
	// (the run's context was done — a serve-layer drain/shutdown or a
	// caller-cancelled request — either before this step started or while it
	// was in flight), or "max_step_visits" (a `goto` cycle with no `break` to
	// escape it, tripping the maxStepVisits safety cap). execsvc's
	// stop_failure hook switches on this instead of string-matching the
	// message — do not add a new failure path without giving it one of
	// these, or extending this set deliberately (and updating every switch
	// over it).
	//
	// Kind is unrelated to a `loop pipeline`'s LoopResult.TerminalReason
	// "max_iterations" — that is a soft, non-error stop (the loop's `max <N>`
	// / `repeat.max_iterations` ceiling was reached on schedule, err == nil)
	// and never produces a StepError at all. The two names are one word
	// apart; do not conflate them.
	Kind string
	Err  error
}

func (e *StepError) Error() string {
	switch e.Kind {
	case "timeout":
		return fmt.Sprintf("runtime: step %q exceeded its timeout: %v", e.Step, e.Err)
	case "cancelled":
		return fmt.Sprintf("runtime: step %q: %v", e.Step, e.Err)
	case "max_step_visits":
		return fmt.Sprintf("runtime: %v", e.Err)
	default:
		return fmt.Sprintf("runtime: step %q failed: %v", e.Step, e.Err)
	}
}

func (e *StepError) Unwrap() error { return e.Err }
