package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

const pipelineSource = `
pipeline AutoFixPipeline {
    input issue_id: string
    input target_file: string

    checkpoint: {
        enabled: true
        strategy: "per_step"
        storage: "file"
        ttl: 7d
    }

    step AuditWithSkill {
        var x = 1
    }

    step RefinementLoop {
        var y = 2
    }
}
`

func parsePipeline(t *testing.T) runtime.Pipeline {
	t.Helper()
	prog, err := parser.Parse(pipelineSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find pipeline: %v", err)
	}
	return p
}

func TestPipelineConfigFromAST(t *testing.T) {
	p := parsePipeline(t)
	if p.Name != "AutoFixPipeline" {
		t.Errorf("name = %q", p.Name)
	}
	if len(p.Steps) != 2 || p.Steps[0] != "AuditWithSkill" || p.Steps[1] != "RefinementLoop" {
		t.Fatalf("steps = %v", p.Steps)
	}
	if !p.Checkpoint.Enabled || p.Checkpoint.Strategy != "per_step" || p.Checkpoint.Storage != "file" {
		t.Errorf("checkpoint config = %+v", p.Checkpoint)
	}
	if p.Checkpoint.TTL != 7*24*time.Hour {
		t.Errorf("ttl = %v, want 168h", p.Checkpoint.TTL)
	}
}

// The `checkpoint { ... }` block is optional: a pipeline that omits it, or
// declares an empty one, is still projected with per-step checkpointing on so
// an interrupted run stays resumable. A partial block tunes fields without
// losing that default; only `enabled: false` opts out.
func TestPipelineCheckpointDefaultsWhenBlockOmittedOrPartial(t *testing.T) {
	cases := []struct {
		name string
		body string
		want runtime.CheckpointConfig
	}{
		{
			name: "no block",
			body: "step S { var x = 1 }",
			want: runtime.DefaultCheckpointConfig(),
		},
		{
			name: "empty block",
			body: "checkpoint: {}\n    step S { var x = 1 }",
			want: runtime.DefaultCheckpointConfig(),
		},
		{
			name: "ttl only keeps per_step",
			body: "checkpoint: { ttl: 30d }\n    step S { var x = 1 }",
			want: runtime.CheckpointConfig{Enabled: true, Strategy: "per_step", TTL: 30 * 24 * time.Hour},
		},
		{
			name: "enabled false opts out",
			body: "checkpoint: { enabled: false }\n    step S { var x = 1 }",
			want: runtime.CheckpointConfig{Enabled: false, Strategy: "per_step"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.Parse("pipeline P {\n    " + tc.body + "\n}\n")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			p, err := runtime.FindPipeline(prog, "")
			if err != nil {
				t.Fatalf("find pipeline: %v", err)
			}
			if p.Checkpoint != tc.want {
				t.Errorf("checkpoint = %+v, want %+v", p.Checkpoint, tc.want)
			}
		})
	}
}

func TestContextConfigFromAST(t *testing.T) {
	const src = `
pipeline WithContext {
    context: {
        source: "session:abc123"
        require: true
    }
    step S { var x = 1 }
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find pipeline: %v", err)
	}
	if p.Context == nil {
		t.Fatal("expected Context to be non-nil")
	}
	if p.Context.Source != "session:abc123" || !p.Context.Require {
		t.Errorf("context config = %+v", p.Context)
	}

	// A pipeline with no context: block leaves Context nil.
	p2 := parsePipeline(t)
	if p2.Context != nil {
		t.Errorf("expected nil Context for a pipeline without the block, got %+v", p2.Context)
	}

	// A bare context: {} still opts in, with Source defaulting to "latest".
	prog3, _ := parser.Parse("pipeline P { context: {}\n step S { var x = 1 } }")
	p3, _ := runtime.FindPipeline(prog3, "")
	if p3.Context == nil || p3.Context.Source != "latest" {
		t.Errorf("bare context block = %+v", p3.Context)
	}
}

// The optional `description: "..."` body property is projected onto
// Pipeline.Description; absent leaves it empty.
func TestPipelineDescriptionFromAST(t *testing.T) {
	prog, _ := parser.Parse(`workflow W {
    description: "Does a useful thing."
    step S { var x = 1 }
}`)
	p, _ := runtime.FindPipeline(prog, "")
	if p.Description != "Does a useful thing." {
		t.Errorf("Description = %q, want %q", p.Description, "Does a useful thing.")
	}

	bare, _ := parser.Parse("pipeline P { step S { var x = 1 } }")
	pb, _ := runtime.FindPipeline(bare, "")
	if pb.Description != "" {
		t.Errorf("Description = %q, want empty for a pipeline with no description:", pb.Description)
	}
}

// AC-6: a checkpoint file is written under .mhl/state per step, and clearing on
// success removes it.
func TestCheckpointSaveWritesFileAndClearRemoves(t *testing.T) {
	root := t.TempDir()
	store := runtime.NewStore(root)

	cp := &runtime.Checkpoint{
		Pipeline:       "AutoFixPipeline",
		LastStep:       "AuditWithSkill",
		NextStep:       "RefinementLoop",
		CompletedSteps: []string{"AuditWithSkill"},
		Variables:      map[string]any{"issue_id": "BUG-102"},
		TTLSeconds:     int64((7 * 24 * time.Hour).Seconds()),
	}
	if err := store.Save(cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(root, runtime.StateDirName, "AutoFixPipeline.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected checkpoint file at %s: %v", path, err)
	}

	loaded, ok, err := store.Load("AutoFixPipeline")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if loaded.LastStep != "AuditWithSkill" || loaded.Variables["issue_id"] != "BUG-102" {
		t.Errorf("loaded checkpoint = %+v", loaded)
	}

	if err := store.Clear("AutoFixPipeline"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected checkpoint file removed after clear")
	}
}

// DR-1 / failure path: an expired (past-TTL) checkpoint is ignored on resume.
func TestExpiredCheckpointIgnored(t *testing.T) {
	root := t.TempDir()
	past := time.Now().Add(-48 * time.Hour)
	store := runtime.NewStore(root).WithClock(func() time.Time { return past })

	cp := &runtime.Checkpoint{
		Pipeline:   "AutoFixPipeline",
		LastStep:   "AuditWithSkill",
		NextStep:   "RefinementLoop",
		TTLSeconds: int64((1 * time.Hour).Seconds()),
	}
	if err := store.Save(cp); err != nil { // saved "48h ago"
		t.Fatalf("save: %v", err)
	}

	// Load with the real clock: the entry is now well past its 1h TTL.
	fresh := runtime.NewStore(root)
	_, ok, err := fresh.Load("AutoFixPipeline")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if ok {
		t.Fatal("expected expired checkpoint to be ignored")
	}
}

// IC-4 / AC-7 / QR-3: crash mid-pipeline after step 1's checkpoint, then resume
// and assert step 1 is not re-executed while step 2 runs.
func TestResumeSkipsCompletedStep(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)

	// First run: step 1 succeeds (and checkpoints); step 2 "crashes".
	var firstRunSteps []string
	runner := runtime.NewRunner(root)
	_, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		firstRunSteps = append(firstRunSteps, step)
		if step == "RefinementLoop" {
			return errSimulatedCrash
		}
		ctx.Vars["done_"+step] = "true"
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected the simulated crash error on first run")
	}
	if len(firstRunSteps) != 2 || firstRunSteps[0] != "AuditWithSkill" {
		t.Fatalf("first run steps = %v", firstRunSteps)
	}

	// The checkpoint from step 1 must survive the crash.
	if _, ok, _ := runtime.NewStore(root).Load(p.Name); !ok {
		t.Fatal("expected a surviving checkpoint after the crash")
	}

	// Resume: step 1 must NOT be re-executed; step 2 must run.
	var resumeSteps []string
	runner2 := runtime.NewRunner(root)
	res, err := runner2.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		resumeSteps = append(resumeSteps, step)
		return nil
	}, true)
	if err != nil {
		t.Fatalf("resume run failed: %v", err)
	}
	if !res.Resumed {
		t.Error("expected Resumed=true")
	}
	if len(resumeSteps) != 1 || resumeSteps[0] != "RefinementLoop" {
		t.Fatalf("resume executed steps = %v, want [RefinementLoop]", resumeSteps)
	}
	for _, s := range resumeSteps {
		if s == "AuditWithSkill" {
			t.Error("completed step AuditWithSkill was re-executed on resume")
		}
	}

	// Restored variable state carries the step-1 result.
	// (Sanity: the checkpoint cleared after successful completion.)
	if _, ok, _ := runtime.NewStore(root).Load(p.Name); ok {
		t.Error("expected checkpoint cleared after successful completion")
	}
}

// TestResumeSkipsInitAndRestoresCheckpointedVars confirms InitFunc runs
// only on a fresh start, never on a resume — a resumed run's ctx.Vars come
// entirely from the checkpoint (including a non-string value, since
// RunContext.Vars is map[string]any specifically so a pipeline var can
// hold one), never from init re-seeding it.
func TestResumeSkipsInitAndRestoresCheckpointedVars(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()

	initCalls := 0
	init := func(ctx *runtime.RunContext) error {
		initCalls++
		ctx.Vars["counter"] = 0.0
		return nil
	}

	runner := runtime.NewRunner(root)
	_, err := runner.Run(context.Background(), p, init, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "A" {
			ctx.Vars["counter"] = 41.0
		}
		if step == "B" {
			return errSimulatedCrash
		}
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected the simulated crash error on first run")
	}
	if initCalls != 1 {
		t.Fatalf("init called %d time(s) on the fresh run, want 1", initCalls)
	}

	runner2 := runtime.NewRunner(root)
	var sawCounter any
	_, err = runner2.Run(context.Background(), p, init, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "B" {
			sawCounter = ctx.Vars["counter"]
		}
		return nil
	}, true)
	if err != nil {
		t.Fatalf("resume run failed: %v", err)
	}
	if initCalls != 1 {
		t.Fatalf("init called %d time(s) total, want 1 (must not run again on resume)", initCalls)
	}
	if sawCounter != 41.0 {
		t.Errorf("counter on resume = %v, want 41.0 (from the checkpoint, not init's 0.0)", sawCounter)
	}
}

var errSimulatedCrash = &crashError{}

type crashError struct{}

func (*crashError) Error() string { return "simulated crash" }

// threeStepPipeline is a bare runtime.Pipeline (no .mh source needed —
// Runner.Run only ever sees step names and checkpoint config) with steps
// A, B, C, per_step checkpointing enabled.
func threeStepPipeline() runtime.Pipeline {
	return runtime.Pipeline{
		Name:       "ThreeStep",
		Steps:      []string{"A", "B", "C"},
		Checkpoint: runtime.CheckpointConfig{Enabled: true, Strategy: "per_step"},
	}
}

// A `goto` redirects execution to any named step, skipping whatever would
// otherwise run next in declaration order.
func TestGotoJumpsToNamedStep(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()
	runner := runtime.NewRunner(root)

	var executed []string
	res, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		executed = append(executed, step)
		if step == "A" {
			return &runtime.GotoSignal{Target: "C"}
		}
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(executed) != 2 || executed[0] != "A" || executed[1] != "C" {
		t.Fatalf("executed = %v, want [A C] (B skipped)", executed)
	}
	if len(res.Executed) != 2 || res.Executed[1] != "C" {
		t.Fatalf("result.Executed = %v", res.Executed)
	}
}

// A `goto` to a name that isn't one of the pipeline's declared steps fails
// closed instead of silently wandering off.
func TestGotoUnknownTargetFails(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()
	runner := runtime.NewRunner(root)

	_, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "A" {
			return &runtime.GotoSignal{Target: "DoesNotExist"}
		}
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected an error for an unknown goto target")
	}
}

// break stops the run outright — remaining steps never execute — and
// carries its reason back on the result, without being reported as a step
// failure.
func TestBreakStopsRunAndPreservesReason(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()
	runner := runtime.NewRunner(root)

	var executed []string
	res, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		executed = append(executed, step)
		if step == "B" {
			return &runtime.BreakSignal{Reason: "too many attempts"}
		}
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(executed) != 2 || executed[1] != "B" {
		t.Fatalf("executed = %v, want [A B] (C never runs)", executed)
	}
	if !res.Broke {
		t.Fatal("expected result.Broke = true")
	}
	if res.BreakReason != "too many attempts" {
		t.Fatalf("BreakReason = %v", res.BreakReason)
	}

	// A break does not clear the checkpoint the way a normal completion
	// does — it's a stop, not a success.
	if _, ok, _ := runtime.NewStore(root).Load(p.Name); !ok {
		t.Error("expected the checkpoint to survive a break")
	}
}

// A break is a clean early exit, so RunResult.FinalVars still carries the
// variable state as it stood at the break — it is not discarded like a
// failure.
func TestBreakKeepsFinalVars(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()
	runner := runtime.NewRunner(root)

	res, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		ctx.Vars["reached_"+step] = true
		if step == "B" {
			return &runtime.BreakSignal{Reason: "stop"}
		}
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.FinalVars == nil {
		t.Fatal("break left FinalVars nil — the run's state was discarded")
	}
	if res.FinalVars["reached_A"] != true || res.FinalVars["reached_B"] != true {
		t.Errorf("FinalVars = %v, want the writes from A and B kept", res.FinalVars)
	}
	if _, ran := res.FinalVars["reached_C"]; ran {
		t.Errorf("FinalVars has reached_C — step C ran past the break")
	}
}

// pause(...) suspends the run: RunResult.Paused is set, its state is kept
// (FinalVars + a checkpoint whose NextStep is the pausing step), and a
// resume re-enters that step — not the next one.
func TestPauseSuspendsAndResumesSameStep(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()

	// Leg 1: pause at B.
	var leg1 []string
	res, err := runtime.NewRunner(root).Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		leg1 = append(leg1, step)
		ctx.Vars["seen_"+step] = true
		if step == "B" {
			return &runtime.PauseSignal{Reason: "awaiting approval"}
		}
		return nil
	}, false)
	if err != nil {
		t.Fatalf("leg1: %v", err)
	}
	if !res.Paused || res.PauseReason != "awaiting approval" {
		t.Fatalf("Paused=%v PauseReason=%v", res.Paused, res.PauseReason)
	}
	if res.Broke {
		t.Error("a pause must not also report Broke")
	}
	if res.FinalVars["seen_B"] != true {
		t.Errorf("FinalVars did not keep the pause-point state: %v", res.FinalVars)
	}
	if leg1[len(leg1)-1] != "B" {
		t.Fatalf("leg1 = %v, want it to end at B", leg1)
	}

	cp, ok, _ := runtime.NewStore(root).Load(p.Name)
	if !ok {
		t.Fatal("pause wrote no checkpoint")
	}
	if cp.NextStep != "B" {
		t.Errorf("checkpoint NextStep = %q, want B (re-enter the pausing step)", cp.NextStep)
	}

	// Leg 2: resume — must land on B again, then run C.
	var leg2 []string
	res2, err := runtime.NewRunner(root).Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		leg2 = append(leg2, step)
		return nil
	}, true)
	if err != nil {
		t.Fatalf("leg2: %v", err)
	}
	if len(leg2) != 2 || leg2[0] != "B" || leg2[1] != "C" {
		t.Fatalf("resume ran %v, want [B C]", leg2)
	}
	if res2.Paused {
		t.Error("resumed run should not still be paused")
	}
}

// The correctness case NextStep exists for: step A jumps to C via goto: the
// checkpoint saved when A finishes must already record NextStep="C", so a
// crash "before" C runs and a resume afterward lands on C — never on B,
// which is only what declaration order would suggest.
func TestResumeAfterGotoFollowsPersistedNextStep(t *testing.T) {
	root := t.TempDir()
	p := threeStepPipeline()

	runner := runtime.NewRunner(root)
	_, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "A" {
			return &runtime.GotoSignal{Target: "C"}
		}
		if step == "C" {
			return errSimulatedCrash // "crash" right as C starts
		}
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected the simulated crash error")
	}

	var resumeExecuted []string
	runner2 := runtime.NewRunner(root)
	res, err := runner2.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		resumeExecuted = append(resumeExecuted, step)
		return nil
	}, true)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !res.Resumed {
		t.Error("expected Resumed = true")
	}
	if len(resumeExecuted) != 1 || resumeExecuted[0] != "C" {
		t.Fatalf("resume executed = %v, want [C] (not B)", resumeExecuted)
	}
}

// A goto cycle with nothing to break out of it fails with a clear error
// instead of hanging mhl run forever.
func TestGotoCycleHitsMaxStepVisits(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{Name: "Cycle", Steps: []string{"A", "B"}}
	runner := runtime.NewRunner(root)

	_, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "A" {
			return &runtime.GotoSignal{Target: "B"}
		}
		return &runtime.GotoSignal{Target: "A"}
	}, false)
	if err == nil {
		t.Fatal("expected a max-visits error for an unbroken goto cycle")
	}
}
