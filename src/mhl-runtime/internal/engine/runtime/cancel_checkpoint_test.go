package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// A run cancelled mid-flight (a serve-layer drain) with checkpointing enabled
// but no per_step strategy still leaves a resume point, so --resume skips the
// completed step.
func TestCancelWritesResumeCheckpointWithoutPerStep(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{
		Name:       "Doc",
		Steps:      []string{"Draft", "Publish"},
		Checkpoint: runtime.CheckpointConfig{Enabled: true}, // no Strategy
	}

	ctx, cancel := context.WithCancel(context.Background())
	var firstRun []string
	_, err := runtime.NewRunner(root).Run(ctx, p, nil,
		func(_ context.Context, step string, rc *runtime.RunContext) error {
			firstRun = append(firstRun, step)
			rc.Vars["did_"+step] = true
			if step == "Draft" {
				cancel() // drain lands after Draft, before Publish
			}
			return nil
		}, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(firstRun) != 1 || firstRun[0] != "Draft" {
		t.Fatalf("first run executed %v, want [Draft]", firstRun)
	}

	cp, ok, cerr := runtime.NewStore(root).Load(p.Name)
	if cerr != nil || !ok {
		t.Fatalf("no resume checkpoint after cancel: ok=%v err=%v", ok, cerr)
	}
	if cp.NextStep != "Publish" {
		t.Errorf("checkpoint NextStep = %q, want Publish", cp.NextStep)
	}

	// Resume: Draft must not re-run.
	var resumeRun []string
	res, err := runtime.NewRunner(root).Run(context.Background(), p, nil,
		func(_ context.Context, step string, _ *runtime.RunContext) error {
			resumeRun = append(resumeRun, step)
			return nil
		}, true)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !res.Resumed || len(resumeRun) != 1 || resumeRun[0] != "Publish" {
		t.Fatalf("resume executed %v (Resumed=%v), want [Publish]", resumeRun, res.Resumed)
	}
}

// With no checkpoint block at all, a cancelled run leaves nothing behind —
// unchanged from before.
func TestCancelWritesNothingWithoutCheckpointBlock(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{Name: "Ephemeral", Steps: []string{"A", "B"}}

	ctx, cancel := context.WithCancel(context.Background())
	_, _ = runtime.NewRunner(root).Run(ctx, p, nil,
		func(_ context.Context, step string, _ *runtime.RunContext) error {
			if step == "A" {
				cancel()
			}
			return nil
		}, false)

	if _, ok, _ := runtime.NewStore(root).Load(p.Name); ok {
		t.Error("a pipeline with no checkpoint block must not leave a checkpoint on cancel")
	}
}
