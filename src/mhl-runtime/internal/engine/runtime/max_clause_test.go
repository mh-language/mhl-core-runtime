package runtime_test

import (
	"context"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// maxOf parses src and returns the projected MaxIterations of its first
// pipeline.
func maxOf(t *testing.T, src string) int {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	return p.MaxIterations
}

// The `max <N>` header clause projects straight onto MaxIterations, the same
// ceiling `repeat { max_iterations: N }` sets.
func TestMaxClauseProjectsOntoMaxIterations(t *testing.T) {
	if got := maxOf(t, `loop workflow Refine max 3 { step S { log("s") } }`); got != 3 {
		t.Fatalf("MaxIterations = %d, want 3", got)
	}
}

// A `repeat` block that carries only a stop_when must not reset the ceiling
// the header clause established.
func TestMaxClauseSurvivesRepeatBlockWithOnlyStopWhen(t *testing.T) {
	src := `loop workflow Refine max 3 {
    repeat: { stop_when: done }
    step S { log("s") }
}`
	if got := maxOf(t, src); got != 3 {
		t.Fatalf("MaxIterations = %d, want 3 (repeat block had no max_iterations)", got)
	}
}

// When both are written, the explicit `repeat { max_iterations }` wins (lint
// separately flags the redundancy).
func TestRepeatMaxIterationsWinsOverMaxClause(t *testing.T) {
	src := `loop workflow Refine max 3 {
    repeat: { max_iterations: 7 }
    step S { log("s") }
}`
	if got := maxOf(t, src); got != 7 {
		t.Fatalf("MaxIterations = %d, want 7", got)
	}
}

// A non-positive value is dropped best-effort (lint reports it), leaving no
// ceiling rather than a nonsensical one.
func TestMaxClauseNonPositiveIsIgnored(t *testing.T) {
	if got := maxOf(t, `loop workflow Refine max 0 { step S { log("s") } }`); got != 0 {
		t.Fatalf("MaxIterations = %d, want 0", got)
	}
}

// End-to-end: a loop declared with `max 2` stops after two iterations with
// TerminalReason "max_iterations".
func TestMaxClauseStopsLoop(t *testing.T) {
	p := maxClausePipeline(t)
	lr := runtime.NewLoopRunner(t.TempDir())
	res, err := lr.Run(context.Background(), p, nil,
		func(_ context.Context, _ string, _ *runtime.RunContext) error { return nil },
		func(string) (bool, error) { return false, nil }, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TerminalReason != "max_iterations" || res.Iterations != 2 {
		t.Fatalf("result = %+v, want max_iterations at 2", res)
	}
}

func maxClausePipeline(t *testing.T) runtime.Pipeline {
	t.Helper()
	prog, err := parser.Parse(`loop workflow Refine max 2 { step S { log("s") } }`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	return p
}
