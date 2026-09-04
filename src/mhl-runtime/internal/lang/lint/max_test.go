package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestMaxClauseValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow Refine max 3 {
    step S { log("s") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for a valid max clause, got %+v", findings)
	}
}

func TestMaxClauseNonPositiveFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow Refine max 0 {
    step S { log("s") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `workflow "Refine": max "0" is not a positive integer`) {
		t.Fatalf("expected a non-positive max finding, got %+v", findings)
	}
}

func TestMaxClauseWithoutLoopFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow Refine max 3 {
    step S { log("s") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `workflow "Refine": max has no effect without the leading `+"`loop`"+` keyword`) {
		t.Fatalf("expected a missing-loop finding, got %+v", findings)
	}
}

func TestMaxClauseAlongsideRepeatMaxIterationsFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow Refine max 3 {
    repeat: { max_iterations: 7 }
    step S { log("s") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `both `+"`max 3`"+` and `+"`repeat { max_iterations }`"+` are set`) {
		t.Fatalf("expected a redundant-ceiling finding, got %+v", findings)
	}
}

// A `repeat` block carrying only a stop_when is not a conflict with `max`.
func TestMaxClauseWithRepeatStopWhenOnlyIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow Refine max 3 {
    repeat: { stop_when: mem_done }
    mem mem_done = false
    step S { log("s") }
}
`)
	findings := lint.File(main)
	for _, f := range findings {
		if strings.Contains(f.Message, "max_iterations") || strings.Contains(f.Message, "max has no effect") {
			t.Fatalf("unexpected max finding: %+v", findings)
		}
	}
}
