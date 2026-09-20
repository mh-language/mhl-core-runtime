package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// TestLintSelfAssignsDeclaredPipelineVarCleanly: self.<name> for a
// genuinely declared input/var is exactly as valid an assignment target as
// the bare name, and lints clean.
func TestLintSelfAssignsDeclaredPipelineVarCleanly(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow Demo {
    input name: string
    var greeting = ""
    step Build {
        self.greeting = "hello " + self.name
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// TestLintSelfAssignsUndeclaredNameFails mirrors checkAssignTarget's
// existing "undefined variable" check for a bare name — self.<name> gets
// the same treatment, worded for self specifically.
func TestLintSelfAssignsUndeclaredNameFails(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow Demo {
    step Build {
        self.nonexistent = "boom"
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, `self.nonexistent: not a declared input, var, or mem`) {
		t.Fatalf("expected a self-undeclared finding, got %+v", findings)
	}
}

// TestLintSelfInsideToolMethodFails: self.<name> (the property form) is
// only valid inside a pipeline/workflow step — inside a `tool` method,
// `self` already means something else entirely (self.method() calling a
// sibling method), so the property form must be rejected there, not
// silently misread.
func TestLintSelfInsideToolMethodFails(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool Helper {
    ping(): string -> {
        self.pong = "not how this works"
        return "ok"
    }
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "only valid inside a pipeline/workflow step, not a tool method") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a self-inside-tool finding, got %+v", findings)
	}
}

// TestLintSelfNestedFieldFails: self.data.nested (two member trailers) and
// self.data[0] (a trailing index) aren't supported target shapes yet —
// only exactly `self.name`, matching the interpreter's own restriction.
func TestLintSelfNestedFieldFails(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow Demo {
    var data = {}
    step Build {
        self.data.nested = "boom"
    }
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "assignment target must be a plain variable or an array index") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a nested-field rejection, got %+v", findings)
	}
}
