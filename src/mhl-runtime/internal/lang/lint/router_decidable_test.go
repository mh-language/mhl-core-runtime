package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A router with neither `select` nor a `decider` can never resolve an
// agent — flagged statically rather than only failing at the first
// `.delegate()` call.
func TestRouterWithNoSelectAndNoDecisionConfigIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
}
`)
	if !hasMessage(lint.File(main), `router "Frontdesk": declares neither select nor a decider`) {
		t.Fatalf("expected the undecidable router to be flagged")
	}
}

// A purely deterministic router — `select` only, no `decider` at all — is a
// legitimate declaration: the LLM cascade is optional.
func TestRouterWithOnlySelectIsAccepted(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    select: (prompt) -> "Billing"
}
`)
	if hasMessage(lint.File(main), "declares neither select nor a decider") {
		t.Fatalf("a select-only router must not be flagged as undecidable")
	}
}

// A router with only a `decider` (no `select`) is also legitimate — select
// is optional too, the router just always decides via the LLM call.
func TestRouterWithOnlyDecisionConfigIsAccepted(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }
agent Decider { command: "claude" }

router Frontdesk {
    agents: [Billing]
    decider: Decider
}
`)
	if hasMessage(lint.File(main), "declares neither select nor a decider") {
		t.Fatalf("a router with a decider must not be flagged as undecidable")
	}
}
