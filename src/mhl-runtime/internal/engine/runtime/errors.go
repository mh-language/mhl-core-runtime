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
	// Kind is "failed" for an ordinary step error and "timeout" for a step
	// that exceeded its `timeout <dur>` clause.
	Kind string
	Err  error
}

func (e *StepError) Error() string {
	if e.Kind == "timeout" {
		return fmt.Sprintf("runtime: step %q exceeded its timeout: %v", e.Step, e.Err)
	}
	return fmt.Sprintf("runtime: step %q failed: %v", e.Step, e.Err)
}

func (e *StepError) Unwrap() error { return e.Err }
