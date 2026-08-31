package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// blockUntilCtx returns as soon as ctx is cancelled (a step `timeout`
// firing), or after d if it never is — the shape a well-behaved blocking
// call inside a step already has.
func blockUntilCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// A step that outruns its `timeout` fails with ErrStepTimeout, does not run
// the steps after it, and — with checkpointing enabled — leaves a resume
// point so `run/resume` re-enters that same step.
func TestStepTimeoutFailsAndCheckpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		strategy string
	}{
		{"generic checkpoint", ""},
		{"per_step", "per_step"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := runtime.Pipeline{
				Name:         "Doc",
				Steps:        []string{"Fast", "Slow", "After"},
				StepTimeouts: map[string]time.Duration{"Slow": 20 * time.Millisecond},
				Checkpoint:   runtime.CheckpointConfig{Enabled: true, Strategy: tc.strategy},
			}

			var ran []string
			_, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
				func(ctx context.Context, step string, rc *runtime.RunContext) error {
					ran = append(ran, step)
					if step == "Slow" {
						return blockUntilCtx(ctx, 2*time.Second)
					}
					return nil
				}, false)

			if !errors.Is(err, runtime.ErrStepTimeout) {
				t.Fatalf("err = %v, want ErrStepTimeout", err)
			}
			if len(ran) != 2 || ran[0] != "Fast" || ran[1] != "Slow" {
				t.Fatalf("ran %v, want [Fast Slow] (After must not run)", ran)
			}

			cp, ok, cerr := runtime.NewStore(root).Load(p.Name)
			if cerr != nil || !ok {
				t.Fatalf("no resume checkpoint after timeout: ok=%v err=%v", ok, cerr)
			}
			if cp.NextStep != "Slow" {
				t.Errorf("checkpoint NextStep = %q, want Slow", cp.NextStep)
			}

			// Resume: Fast is skipped, Slow re-runs (and this time is quick).
			var resumeRan []string
			res, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
				func(_ context.Context, step string, _ *runtime.RunContext) error {
					resumeRan = append(resumeRan, step)
					return nil
				}, true)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if !res.Resumed || len(resumeRan) != 2 || resumeRan[0] != "Slow" || resumeRan[1] != "After" {
				t.Fatalf("resume ran %v (Resumed=%v), want [Slow After]", resumeRan, res.Resumed)
			}
		})
	}
}

// Without a checkpoint block, a step timeout still fails the run with
// ErrStepTimeout but leaves nothing resumable — same as a bare cancel.
func TestStepTimeoutNoCheckpointWhenDisabled(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{
		Name:         "Ephemeral",
		Steps:        []string{"Slow", "After"},
		StepTimeouts: map[string]time.Duration{"Slow": 15 * time.Millisecond},
	}

	_, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
		func(ctx context.Context, step string, _ *runtime.RunContext) error {
			if step == "Slow" {
				return blockUntilCtx(ctx, time.Second)
			}
			return nil
		}, false)

	if !errors.Is(err, runtime.ErrStepTimeout) {
		t.Fatalf("err = %v, want ErrStepTimeout", err)
	}
	if _, ok, _ := runtime.NewStore(root).Load(p.Name); ok {
		t.Error("a pipeline with no checkpoint block must not leave a checkpoint on a step timeout")
	}
}

// A step that finishes well inside its `timeout` is unaffected.
func TestStepUnderTimeoutCompletes(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{
		Name:         "P",
		Steps:        []string{"Work", "Done"},
		StepTimeouts: map[string]time.Duration{"Work": time.Second},
	}

	var ran []string
	res, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
		func(_ context.Context, step string, _ *runtime.RunContext) error {
			ran = append(ran, step)
			return nil
		}, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Executed) != 2 || ran[0] != "Work" || ran[1] != "Done" {
		t.Fatalf("ran %v, want [Work Done]", ran)
	}
}

// A `timeout` on the `parallel` group header cancels every still-running
// branch when it elapses, and the barrier fails with ErrStepTimeout.
func TestParallelGroupTimeoutCancelsBranches(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{
		Name:         "P",
		Steps:        []string{"quick", "slow"},
		Stages:       []runtime.Stage{{Name: "Group", Steps: []string{"quick", "slow"}, Parallel: true}},
		StepTimeouts: map[string]time.Duration{"Group": 20 * time.Millisecond},
	}

	_, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
		func(ctx context.Context, step string, _ *runtime.RunContext) error {
			if step == "slow" {
				return blockUntilCtx(ctx, 2*time.Second)
			}
			return nil
		}, false)

	if !errors.Is(err, runtime.ErrStepTimeout) {
		t.Fatalf("err = %v, want ErrStepTimeout", err)
	}
}

// A `timeout` on a single branch step fails just that branch (and so the
// group) with ErrStepTimeout, without a group-level clause.
func TestParallelBranchTimeout(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{
		Name:         "P",
		Steps:        []string{"quick", "slow"},
		Stages:       []runtime.Stage{{Name: "Group", Steps: []string{"quick", "slow"}, Parallel: true}},
		StepTimeouts: map[string]time.Duration{"slow": 20 * time.Millisecond},
	}

	_, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
		func(ctx context.Context, step string, _ *runtime.RunContext) error {
			if step == "slow" {
				return blockUntilCtx(ctx, 2*time.Second)
			}
			return nil
		}, false)

	if !errors.Is(err, runtime.ErrStepTimeout) {
		t.Fatalf("err = %v, want ErrStepTimeout", err)
	}
}
