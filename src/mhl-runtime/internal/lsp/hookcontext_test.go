package lsp

import "testing"

// TestHookParamCompletionOffersSessionContextFields proves `s.` inside
// session_start's/session_end's own lambda body offers SessionContext's
// fields, using whatever identifier the user actually wrote as the
// parameter name (not a fixed "session").
func TestHookParamCompletionOffersSessionContextFields(t *testing.T) {
	for _, hook := range []string{"session_start", "session_end"} {
		t.Run(hook, func(t *testing.T) {
			src, pos := posAtMarker(t, `
pipeline P {
    `+hook+`: (s) -> { s.§ }
    step S {}
}
`)
			items := completionAt("main.mh", src, pos)
			for _, field := range []string{"pipeline", "kind", "session_id", "resumed", "inputs", "vars", "broke", "break_reason", "iterations"} {
				if !hasLabel(items, field) {
					t.Errorf("%s: missing SessionContext field %q in completion", hook, field)
				}
			}
			if hasLabel(items, "step") {
				t.Errorf("%s: unexpected StepContext field leaking into SessionContext completion", hook)
			}
		})
	}
}

// TestHookParamCompletionOffersStepContextFields covers step_start/step_end.
func TestHookParamCompletionOffersStepContextFields(t *testing.T) {
	for _, hook := range []string{"step_start", "step_end"} {
		t.Run(hook, func(t *testing.T) {
			src, pos := posAtMarker(t, `
pipeline P {
    `+hook+`: (step) -> { step.§ }
    step S {}
}
`)
			items := completionAt("main.mh", src, pos)
			for _, field := range []string{"pipeline", "step", "index", "total", "error"} {
				if !hasLabel(items, field) {
					t.Errorf("%s: missing StepContext field %q in completion", hook, field)
				}
			}
			if hasLabel(items, "resumed") {
				t.Errorf("%s: unexpected SessionContext field leaking into StepContext completion", hook)
			}
		})
	}
}

// TestHookParamCompletionOffersFailureContextFields covers stop_failure.
func TestHookParamCompletionOffersFailureContextFields(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    stop_failure: (failure) -> { failure.§ }
    step S {}
}
`)
	items := completionAt("main.mh", src, pos)
	for _, field := range []string{"pipeline", "kind", "step", "error", "reason"} {
		if !hasLabel(items, field) {
			t.Errorf("missing FailureContext field %q in completion", field)
		}
	}
}

// TestHookParamCompletionUsesTheDeclaredParamName proves the field list is
// keyed off whatever identifier the user actually bound — a differently
// named identifier that happens to share a hook's conventional name (e.g.
// "step" used as session_start's own parameter) must NOT get field
// completion, since it isn't session_start's real parameter.
func TestHookParamCompletionUsesTheDeclaredParamName(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    session_start: (ctx) -> { ctx.§ }
    step S {}
}
`)
	items := completionAt("main.mh", src, pos)
	if !hasLabel(items, "resumed") {
		t.Error("expected SessionContext fields for the declared parameter name \"ctx\"")
	}

	src2, pos2 := posAtMarker(t, `
pipeline P {
    session_start: (ctx) -> { other.§ }
    step S {}
}
`)
	items2 := completionAt("main.mh", src2, pos2)
	if hasLabel(items2, "resumed") {
		t.Error("must not offer SessionContext fields for an identifier that isn't the hook's own parameter")
	}
}

// TestHookParamCompletionDoesNotLeakOutsideTheHookLambda proves a step body
// with an identifier that happens to be named like a hook parameter gets
// ordinary (empty/nil) completion, not SessionContext's fields — the block
// classifier must not misfire once the cursor has left the hook's lambda.
func TestHookParamCompletionDoesNotLeakOutsideTheHookLambda(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    session_start: (s) -> { log(s.session_id) }
    step S { s.§ }
}
`)
	items := completionAt("main.mh", src, pos)
	if hasLabel(items, "resumed") {
		t.Error("SessionContext completion leaked outside session_start's own lambda body")
	}
}

// TestTypeAnnotationCompletionOffersHookContextTypes proves
// SessionContext/StepContext/FailureContext are offered as real type names
// in an ordinary `: Type` position, not just inferred for a hook parameter.
func TestTypeAnnotationCompletionOffersHookContextTypes(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    input session: §
    step S {}
}
`)
	items := completionAt("main.mh", src, pos)
	for _, want := range []string{"SessionContext", "StepContext", "FailureContext"} {
		if !hasLabel(items, want) {
			t.Errorf("missing builtin type %q in type-annotation completion", want)
		}
	}
}
