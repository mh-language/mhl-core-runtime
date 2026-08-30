package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// Run checks its context at every step boundary: once cancelled, the next
// step does not run and Run returns a context error.
func TestRunStopsAtStepBoundaryOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := runtime.Pipeline{Name: "P", Steps: []string{"A", "B"}}

	var ran []string
	_, err := runtime.NewRunner(t.TempDir()).Run(ctx, p, nil,
		func(_ context.Context, step string, _ *runtime.RunContext) error {
			ran = append(ran, step)
			if step == "A" {
				cancel()
			}
			return nil
		}, false)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(ran) != 1 || ran[0] != "A" {
		t.Fatalf("ran = %v, want [A] — B must not run after cancel", ran)
	}
}

// An already-cancelled context stops Run before the first step.
func TestRunCancelledBeforeFirstStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := runtime.Pipeline{Name: "P", Steps: []string{"A"}}

	_, err := runtime.NewRunner(t.TempDir()).Run(ctx, p, nil,
		func(_ context.Context, _ string, _ *runtime.RunContext) error {
			t.Fatal("step must not run when the context is already cancelled")
			return nil
		}, false)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// LoopRunner checks the context between iterations.
func TestLoopRunStopsBetweenIterationsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := runtime.Pipeline{Name: "Cycle", Steps: []string{"Only"}, Loop: true, MaxIterations: 10}

	calls := 0
	_, err := runtime.NewLoopRunner(t.TempDir()).Run(ctx, p, nil,
		func(_ context.Context, _ string, _ *runtime.RunContext) error {
			calls++
			cancel()
			return nil
		},
		func(string) (bool, error) { return false, nil },
		false)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("step ran %d times, want 1 — the loop must not start iteration 2 after cancel", calls)
	}
}
