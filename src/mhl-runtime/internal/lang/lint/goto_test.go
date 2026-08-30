package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// `goto` in a plain `pipeline` is a lint error — the linear-order guarantee
// is the whole reason `pipeline` and `workflow` are separate keywords.
func TestGotoInPipelineIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step A { goto B }
    step B { log("b") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, "`goto` is only valid inside a `workflow`") {
		t.Fatalf("expected a goto-in-pipeline finding, got %+v", findings)
	}
}

// The same source is clean once the keyword is `workflow`.
func TestGotoInWorkflowIsAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    step A { goto B }
    step B { log("b") }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "goto") {
			t.Fatalf("unexpected goto finding for a workflow: %+v", f)
		}
	}
}

// A `goto` target that names no step of the same declaration is caught
// statically rather than only at run time.
func TestGotoUnknownTargetInWorkflowIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    step A { goto Nope }
    step B { log("b") }
}
`)
	if !hasMessage(lint.File(main), "targets a step that isn't declared in it") {
		t.Fatalf("expected an unknown-goto-target finding")
	}
}

// `loop workflow` is still a workflow — `goto` stays legal under the `loop`
// prefix.
func TestGotoInLoopWorkflowIsAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow W {
    repeat: { max_iterations: 3 }
    step A { goto B }
    step B { log("b") }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "goto") {
			t.Fatalf("unexpected goto finding for a loop workflow: %+v", f)
		}
	}
}
