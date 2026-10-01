package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A `.select(...)` call against a router that was never declared is a
// finding, mirroring TestCheckRouterNotFound.
func TestCheckRouterSelectNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var chosen = Ghost.select(prompt: "hi")
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

// `.select(...)` requires a non-empty `prompt:` argument, mirroring
// `.delegate(...)`'s equivalent check.
func TestCheckRouterSelectRequiresPrompt(t *testing.T) {
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
        var chosen = Frontdesk.select()
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `Frontdesk.select requires a non-empty prompt`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// `.select(...)` on a router that only declares a `decider` (no `select:`
// hook at all) is a dedicated finding, caught statically rather than left as
// a runtime error at the first call site.
func TestCheckRouterSelectOnDeciderOnlyRouterIsFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }
agent Decider { command: "claude" }

router Frontdesk {
    agents: [Billing]
    decider: Decider
}

pipeline P {
    step S {
        var chosen = Frontdesk.select(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "no select hook declared") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// A well-formed `.select(...)` call against a router that declares `select:`
// produces no findings.
func TestCheckRouterSelectCallIsClean(t *testing.T) {
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
        var chosen = Frontdesk.select(prompt: "hi")
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}
