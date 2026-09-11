package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A router's `decider: ...` entry naming an undeclared agent is a finding,
// mirroring TestRouterAgentRefIsCheckedAgainstDeclaredAgents.
func TestRouterDeciderRefIsCheckedAgainstDeclaredAgents(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    decider: Nope
}
`)
	if !hasMessage(lint.File(main), `router "Frontdesk": decider agent "Nope" is not declared`) {
		t.Fatalf("expected the undeclared decider reference to be flagged")
	}
}

// A router referencing a declared decider agent produces no such finding.
func TestRouterDeciderRefIsCleanWhenDeclared(t *testing.T) {
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
	if hasMessage(lint.File(main), "is not declared") {
		t.Fatalf("unexpected undeclared-decider finding for a fully declared router")
	}
}

// An inline `decider: agent { ... }` literal is checked for unknown
// properties the same way an inline fallback agent already is.
func TestRouterInlineDeciderPropertiesAreChecked(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    decider: agent {
        command: "claude"
        timeout: 30
    }
}
`)
	if !hasMessage(lint.File(main), `unknown property "timeout"`) {
		t.Fatalf("expected the inline decider agent's timeout to be flagged")
	}
}

// A well-formed inline decider literal produces no findings.
func TestRouterInlineDeciderIsCleanWhenValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    decider: agent { command: "claude" }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}
