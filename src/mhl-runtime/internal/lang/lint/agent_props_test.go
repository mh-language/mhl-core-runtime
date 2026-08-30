package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A property the runtime never reads (a typo, or a docs-only field like
// `api_key`) fails `mhl lint` instead of being silently ignored.
func TestUnknownAgentPropertyIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Claude {
    command: "claude"
    args: ["-p", "${prompt}"]
    api_key: "sk-xxx"
    system_instructions: "be terse"
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `unknown property "api_key"`) {
		t.Fatalf("expected api_key to be flagged, got %+v", findings)
	}
	if !hasMessage(findings, `unknown property "system_instructions"`) {
		t.Fatalf("expected system_instructions to be flagged, got %+v", findings)
	}
}

// Every property the runtime actually reads is accepted.
func TestKnownAgentPropertiesAreClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Full {
    engine: "cli/claude-code"
    command: "claude"
    args: ["-p", "${prompt}"]
    log: "run.log"
    trace: true
    retry: { max_attempts: 2 }
    cache: { ttl: 1h }
    rate_limit: { requests_per_minute: 30 }
    fallback: [Backup]
    before: () -> { return {} }
    after: () -> { log("done") }
}

agent Backup { command: "echo" }
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "unknown property") {
			t.Fatalf("unexpected unknown-property finding: %+v", f)
		}
	}
}

// An inline `fallback: [agent { ... }]` literal is checked the same way.
func TestUnknownPropertyInInlineFallbackAgentIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Primary {
    command: "claude"
    fallback: [
        agent {
            command: "echo"
            timeout: 30
        }
    ]
}
`)
	if !hasMessage(lint.File(main), `unknown property "timeout"`) {
		t.Fatalf("expected the inline fallback agent's `timeout` to be flagged")
	}
}
