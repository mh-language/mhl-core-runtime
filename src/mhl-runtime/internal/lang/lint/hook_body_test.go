package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// TestHookBodyFlagsUndefinedZeroTrailerIdentifier is the exact real bug that
// motivated this check: a step_end hook referenced an undefined identifier
// as a plain object-literal field value, silently swallowed at runtime by
// the hook's own try/catch — mhl lint used to report nothing at all.
func TestHookBodyFlagsUndefinedZeroTrailerIdentifier(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step_end: (ctx: StepContext) -> {
        try {
            var data = { seen: undeclared_name }
        } catch (e) {
            log.error("swallowed: ${e}")
        }
    }
    step Only { log.info("running") }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, `undefined identifier "undeclared_name"`) {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

// A pipeline input, var, or mem, read as a plain identifier inside a hook,
// is never flagged — the exact case this check exists to allow.
func TestHookBodyDoesNotFlagPipelineInputVarOrMem(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    input project_id: string
    var greeting = "hi"
    mem counter = 0
    step_end: (ctx: StepContext) -> {
        log.info("${project_id} ${greeting} ${counter}")
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// self.<input> inside a hook is not flagged — this now resolves at runtime
// (the RunPipelineHook pipelineEnv fix), and self is always an allowed
// builtin regardless.
func TestHookBodyDoesNotFlagSelf(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    input project_id: string
    step_end: (ctx: StepContext) -> {
        log.info(self.project_id)
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// A catch(e) binding is not flagged when read inside its own catch block —
// regression guard for a real gap this check would otherwise have exposed:
// collectVarNames never recorded a Try statement's ErrName.
func TestHookBodyDoesNotFlagCatchBinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step_end: (ctx: StepContext) -> {
        try {
            fail("boom")
        } catch (e) {
            log.error("caught: ${e}")
        }
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// A var declared earlier in the hook body, a for-loop element, the hook's
// own parameter, and context are all recognized as declared.
func TestHookBodyDoesNotFlagLocallyDeclaredNames(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step_end: (ctx: StepContext) -> {
        var total = 0
        for (var n in [1, 2, 3]) {
            total = total + n
        }
        log.info("${ctx.step} ${total} ${context}")
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// A declared top-level name (an agent, in nameof's target position) is not
// flagged even though it's a zero-trailer bare identifier — the same shape
// checkNameofCallShape already validates independently.
func TestHookBodyDoesNotFlagNameofTarget(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }
pipeline P {
    step_end: (ctx: StepContext) -> {
        log.info(nameof(Billing))
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// An undefined identifier reachable only through a `match` arm or a `with`
// override inside the hook body is still caught — completeness, not just
// the top-level object-literal case.
func TestHookBodyChecksInsideMatchAndWith(t *testing.T) {
	cases := map[string]string{
		"match arm": `
pipeline P {
    step_end: (ctx: StepContext) -> {
        var x = match ctx.step { _ -> bogus_one }
    }
    step Only { log.info("running") }
}
`,
		"with override": `
pipeline P {
    step_end: (ctx: StepContext) -> {
        var base = { a: 1 }
        var x = base with { b: bogus_two }
    }
    step Only { log.info("running") }
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.mh")
			write(t, main, src)
			findings := lint.File(main)
			if len(findings) != 1 || !strings.Contains(findings[0].Message, "undefined identifier") {
				t.Fatalf("unexpected findings: %+v", findings)
			}
		})
	}
}

// An identifier used only inside a NESTED lambda's own body is never
// flagged, even when it's genuinely undefined — deliberately out of scope
// (see checkPipelineHookBody's doc comment): the lambda's own parameter
// introduces a local scope this check does not attempt to track, so it
// stays silent there rather than risk a false positive.
func TestHookBodyDoesNotDescendIntoNestedLambdas(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step_end: (ctx: StepContext) -> {
        var mapped = [1, 2, 3].map((n) -> n + genuinely_undefined)
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings (nested lambda bodies are out of scope), got %+v", findings)
	}
}

// An identifier that's the target of a member access or call — an agent,
// tool, memory, or router name — is never flagged even when undeclared:
// that surface belongs to the narrower, existing shape checks
// (checkAgentCalls, checkRouterDelegateCallShape, ...), not this one.
func TestHookBodyDoesNotFlagCallOrMemberTargets(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step_end: (ctx: StepContext) -> {
        log.info(SomeUndeclaredThing.method())
    }
    step Only { log.info("running") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings (call/member targets are out of this check's scope), got %+v", findings)
	}
}

// A partial workflow fragment whose hook references a name declared in a
// SIBLING fragment (not yet merged, EntryStepCount() != 1) is not flagged —
// this is exactly the real discovery.partial.hook.mh shape (self.artifact/
// self.project_id declared in a different file than the hook itself).
func TestHookBodySkipsIncompletePartialWorkflow(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
partial workflow P {
    step_end: (ctx: StepContext) -> {
        log.info(self.project_id)
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings on an incomplete partial workflow fragment, got %+v", findings)
	}
}
