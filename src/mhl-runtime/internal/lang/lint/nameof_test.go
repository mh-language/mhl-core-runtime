package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// nameof(Undeclared) is a static error wherever it's reachable by lint's
// expression walk (a pipeline step here), catching a typo before the
// program is ever run.
func TestNameofUndeclaredNameIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }

pipeline P {
    step S {
        var name = nameof(Biling)
    }
}
`)
	if !hasMessage(lint.File(main), `nameof: "Biling" is not a declared name`) {
		t.Fatalf("expected the undeclared nameof argument to be flagged")
	}
}

// nameof(Billing), where Billing really is declared, produces no finding.
func TestNameofDeclaredNameIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }

pipeline P {
    step S {
        var name = nameof(Billing)
    }
}
`)
	if hasMessage(lint.File(main), "nameof") {
		t.Fatalf("did not expect a nameof finding for a declared name")
	}
}

// A string literal (or any other non-identifier expression) is rejected —
// nameof only accepts a bare declared name.
func TestNameofRejectsNonIdentifierArgument(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var name = nameof("Billing")
    }
}
`)
	if !hasMessage(lint.File(main), "nameof's argument must be a bare declared name") {
		t.Fatalf("expected the string-literal argument to be flagged")
	}
}

// A router's select hook is statically walked too: a typo'd nameof(...)
// inside it is caught by mhl lint, not just at the first .delegate() call.
func TestNameofTypoInsideRouterSelectIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }
agent Support { command: "echo" }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return nameof(Biling) }
        return nameof(Support)
    }
}
`)
	if !hasMessage(lint.File(main), `nameof: "Biling" is not a declared name`) {
		t.Fatalf("expected the typo inside select to be flagged")
	}
}

// The same select body, with the typo fixed, is clean — proving the walk
// isn't just always-flag.
func TestNameofInsideRouterSelectIsCleanWhenCorrect(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }
agent Support { command: "echo" }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return nameof(Billing) }
        return nameof(Support)
    }
}
`)
	if hasMessage(lint.File(main), "nameof") {
		t.Fatalf("did not expect a nameof finding, got findings: %+v", lint.File(main))
	}
}

// A single-expression select body (no braces) is also statically walked.
func TestNameofInsideRouterSelectExpressionBodyIsChecked(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }

router Frontdesk {
    agents: [Billing]
    select: (prompt) -> nameof(Biling)
}
`)
	if !hasMessage(lint.File(main), `nameof: "Biling" is not a declared name`) {
		t.Fatalf("expected the typo in the single-expression select body to be flagged")
	}
}

// checkRouterSelectBody reuses the same statement/expression walker pipeline
// steps do, so a call to an undeclared agent inside select is caught the
// same way it would be inside a pipeline step.
func TestAgentNotFoundInsideRouterSelectIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "echo" }

router Frontdesk {
    agents: [Billing]
    select: (prompt) -> {
        Ghost.run(prompt: "hi")
        return nameof(Billing)
    }
}
`)
	if !hasMessage(lint.File(main), `agent "Ghost" not found`) {
		t.Fatalf("expected the undeclared agent reference inside select to be flagged, got: %+v", lint.File(main))
	}
}
