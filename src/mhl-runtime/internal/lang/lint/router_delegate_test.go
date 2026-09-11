package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A `.delegate(...)` call against a router that was never declared is a
// finding, mirroring TestCheckAgentNotFound.
func TestCheckRouterNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var response = Ghost.delegate(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `router "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// `.delegate(...)` requires a non-empty `prompt:` argument, mirroring the
// equivalent `.run(...)` check.
func TestCheckRouterDelegateRequiresPrompt(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    select: (prompt) -> "Billing"
}

pipeline P {
    step S {
        var response = Frontdesk.delegate()
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `Frontdesk.delegate requires a non-empty prompt`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// A well-formed `.delegate(...)` call against a fully declared router
// produces no findings.
func TestCheckRouterDelegateCallIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    select: (prompt) -> "Billing"
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "hi")
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}
