package lint_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A property the runtime never reads (a typo, or a docs-only field) fails
// `mhl lint` instead of being silently ignored, matching
// TestUnknownAgentPropertyIsRejected.
func TestUnknownRouterPropertyIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
    description: "not a real router property"
}
`)
	if !hasMessage(lint.File(main), `unknown property "description"`) {
		t.Fatalf("expected description to be flagged")
	}
}

// Every property the runtime actually reads is accepted.
func TestKnownRouterPropertiesAreClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }
agent Support { command: "claude" }
agent Decider { command: "claude" }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> { return null }
    decider: Decider
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "unknown property") {
			t.Fatalf("unexpected unknown-property finding: %+v", f)
		}
	}
}

// Every entry of the shared ast.RouterBodyProperties allow-list must be
// accepted by lint, mirroring TestEveryDeclaredBodyPropertyIsAccepted.
func TestEveryDeclaredRouterBodyPropertyIsAccepted(t *testing.T) {
	dir := t.TempDir()
	for _, p := range ast.RouterBodyProperties {
		main := filepath.Join(dir, p.Name+".mh")
		write(t, main, fmt.Sprintf(`
router Frontdesk {
    %s: { }
}
`, p.Name))
		for _, f := range lint.File(main) {
			if strings.Contains(f.Message, fmt.Sprintf("unknown property %q", p.Name)) {
				t.Errorf("lint rejects declared body property %q: %s", p.Name, f.Message)
			}
		}
	}
}

// A router's `agents: [...]` entry that doesn't name a declared agent is a
// finding.
func TestRouterAgentRefIsCheckedAgainstDeclaredAgents(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
router Frontdesk {
    agents: [Nope]
}
`)
	if !hasMessage(lint.File(main), `agent "Nope" is not declared`) {
		t.Fatalf("expected the undeclared agent reference to be flagged")
	}
}

// A router referencing only declared agents produces no such finding.
func TestRouterAgentRefsAreCleanWhenDeclared(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Billing { command: "claude" }

router Frontdesk {
    agents: [Billing]
}
`)
	if hasMessage(lint.File(main), "is not declared") {
		t.Fatalf("unexpected undeclared-agent finding for a fully declared router")
	}
}
