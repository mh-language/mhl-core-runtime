package lsp

import "testing"

// TestParamTypeCompletionOffersSessionContextFieldsInToolMethod is the
// regression test for the reported gap: SessionContext (or any builtin
// context type) used as an ordinary tool-method parameter — not a
// session_start/session_end lifecycle hook's own lambda parameter — must
// still offer field completion, since mhl lint/mhl run already accept
// `session.vars` there unconditionally.
func TestParamTypeCompletionOffersSessionContextFieldsInToolMethod(t *testing.T) {
	src, pos := posAtMarker(t, `
tool T {
    log_session(session: SessionContext): string -> {
        session.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	for _, field := range []string{"pipeline", "kind", "session_id", "resumed", "inputs", "vars", "broke", "break_reason", "iterations"} {
		if !hasLabel(items, field) {
			t.Errorf("missing SessionContext field %q in completion", field)
		}
	}
}

// TestParamTypeCompletionUsesTheEnclosingMethodsOwnParam proves a tool with
// several methods resolves target against the *specific* method containing
// the cursor, not just any method sharing a parameter name.
func TestParamTypeCompletionUsesTheEnclosingMethodsOwnParam(t *testing.T) {
	src, pos := posAtMarker(t, `
tool T {
    a(step: StepContext): string -> {
        return step.step
    }

    b(failure: FailureContext): string -> {
        failure.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	for _, field := range []string{"pipeline", "kind", "step", "error", "reason"} {
		if !hasLabel(items, field) {
			t.Errorf("missing FailureContext field %q in completion", field)
		}
	}
	if hasLabel(items, "resumed") {
		t.Error("unexpected SessionContext field leaking into FailureContext completion")
	}
}

// TestParamTypeCompletionResolvesAUserTypeAlias proves a tool-method
// parameter typed with the current file's own `type X = ...` alias — not
// just a builtin like SessionContext — also gets field completion, via
// types.Aliases rather than objectTypeFieldItems' types.Parse-only path.
func TestParamTypeCompletionResolvesAUserTypeAlias(t *testing.T) {
	src, pos := posAtMarker(t, `
type Job = { id: string, retries: number }

tool T {
    describe(job: Job): string -> {
        job.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	for _, field := range []string{"id", "retries"} {
		if !hasLabel(items, field) {
			t.Errorf("missing Job field %q in completion", field)
		}
	}
}

// TestParamTypeCompletionDoesNotLeakOutsideItsOwnMethod proves an
// identifier that merely happens to share a typed parameter's name in a
// sibling method — or isn't a parameter at all — gets no field completion.
func TestParamTypeCompletionDoesNotLeakOutsideItsOwnMethod(t *testing.T) {
	src, pos := posAtMarker(t, `
tool T {
    a(session: SessionContext): string -> {
        return session.vars
    }

    b(): string -> {
        session.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	if hasLabel(items, "resumed") {
		t.Error("SessionContext completion leaked into a method where \"session\" isn't a parameter")
	}
}

// TestParamTypeCompletionRequiresAnExplicitTypeAnnotation proves an
// untyped parameter (no ": Type" at all) gets no field completion — there
// is nothing to resolve, the same as an unannotated var today.
func TestParamTypeCompletionRequiresAnExplicitTypeAnnotation(t *testing.T) {
	src, pos := posAtMarker(t, `
tool T {
    log_session(session): string -> {
        session.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	if hasLabel(items, "resumed") {
		t.Error("unexpected field completion for a parameter with no type annotation")
	}
}
