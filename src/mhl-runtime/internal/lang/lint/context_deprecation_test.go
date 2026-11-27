package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// `context: { ... }` and every `context.vars` read are deprecated in favour
// of `mem`; the run metadata (`context.session_id`, ...) is not.
func TestContextVarsIsDeprecated(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, `
workflow W {
    context: { source: "latest" }
    var last = null
    output: { prior: context.vars }
    on session_start(session) { log(context.vars) }
    step S {
        last = context.vars["x"]
        log(context.session_id, context.started_at, context.resumed, context.principal)
    }
}
`)
	var block, reads int
	for _, f := range lint.File(main) {
		switch {
		case strings.Contains(f.Message, "`context: { ... }` is deprecated"):
			block++
		case strings.Contains(f.Message, "`context.vars` is deprecated"):
			reads++
		default:
			t.Errorf("unexpected finding: %+v", f)
			continue
		}
		if f.Severity != lint.SeverityWarning {
			t.Errorf("deprecation is not a warning: %+v", f)
		}
	}
	if block != 1 {
		t.Errorf("block warnings = %d, want 1", block)
	}
	// output:, the session_start hook and the step each read context.vars once.
	if reads != 3 {
		t.Errorf("context.vars warnings = %d, want 3", reads)
	}
}

// Run metadata needs no block and is not deprecated.
func TestContextMetadataIsNotDeprecated(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, `
workflow W {
    step S { log(context.session_id, context.started_at, context.resumed, context.principal) }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}
