package runtime_test

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// PipelineFromAST projects a `parallel` group onto one Parallel stage while
// still flattening its branch names into Steps.
func TestPipelineFromASTBuildsParallelStage(t *testing.T) {
	prog, err := parser.Parse(`
pipeline P {
    step Pre { var x = 1 }
    parallel Gather {
        step A { var x = 1 }
        step B { var x = 1 }
    }
    step Post { var x = 1 }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got, want := p.Steps, []string{"Pre", "A", "B", "Post"}; !equalStrings(got, want) {
		t.Fatalf("Steps = %v, want %v", got, want)
	}
	if len(p.Stages) != 3 {
		t.Fatalf("Stages = %+v, want 3", p.Stages)
	}
	if p.Stages[1].Name != "Gather" || !p.Stages[1].Parallel ||
		!equalStrings(p.Stages[1].Steps, []string{"A", "B"}) {
		t.Fatalf("Stages[1] = %+v", p.Stages[1])
	}
	if p.Stages[0].Parallel || p.Stages[2].Parallel {
		t.Fatalf("singleton stages should not be Parallel: %+v", p.Stages)
	}
}

func parallelStagePipeline() runtime.Pipeline {
	return runtime.Pipeline{
		Name:  "Par",
		Steps: []string{"A", "B"},
		Stages: []runtime.Stage{
			{Name: "G", Steps: []string{"A", "B"}, Parallel: true},
		},
	}
}

// The branch steps of a group run at the same time, not one after another.
func TestParallelStageRunsConcurrently(t *testing.T) {
	root := t.TempDir()
	runner := runtime.NewRunner(root)

	start := time.Now()
	_, err := runner.Run(parallelStagePipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		time.Sleep(150 * time.Millisecond)
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("two 150ms branch steps took %s — they did not run concurrently", elapsed)
	}
}

// On join, each branch's writes are merged back into the shared vars.
func TestParallelStageMergesBranchWrites(t *testing.T) {
	root := t.TempDir()
	runner := runtime.NewRunner(root)

	res, err := runner.Run(parallelStagePipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		ctx.Vars[step] = "wrote-" + step
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.FinalVars["A"] != "wrote-A" || res.FinalVars["B"] != "wrote-B" {
		t.Fatalf("FinalVars = %v, want both branch writes merged", res.FinalVars)
	}
}

// Two branches assigning the same var to different values is a hard error —
// a group's variable outcome must be deterministic.
func TestParallelStageConflictingWritesFail(t *testing.T) {
	root := t.TempDir()
	runner := runtime.NewRunner(root)

	_, err := runner.Run(parallelStagePipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		ctx.Vars["shared"] = step // "A" vs "B"
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected a conflict error when two branches write the same var differently")
	}
}

// Two branches assigning the same var to the *same* value is fine.
func TestParallelStageSameValueNoConflict(t *testing.T) {
	root := t.TempDir()
	runner := runtime.NewRunner(root)

	res, err := runner.Run(parallelStagePipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		ctx.Vars["shared"] = "agreed"
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.FinalVars["shared"] != "agreed" {
		t.Fatalf("FinalVars[shared] = %v", res.FinalVars["shared"])
	}
}

// The join is fail-slow: a failing branch does not stop the others, and the
// branch error is surfaced once everything has settled.
func TestParallelStageFailSlowSurfacesBranchError(t *testing.T) {
	root := t.TempDir()
	runner := runtime.NewRunner(root)

	var mu sync.Mutex
	var ran []string
	_, err := runner.Run(parallelStagePipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		if step == "A" {
			return fmt.Errorf("branch A boom")
		}
		time.Sleep(50 * time.Millisecond) // B finishes after A already errored
		mu.Lock()
		ran = append(ran, step)
		mu.Unlock()
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected the failing branch's error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "B" {
		t.Fatalf("ran = %v, want [B] — the healthy branch should still complete", ran)
	}
}

// A crash inside one branch leaves no post-group checkpoint, so --resume
// re-runs the whole group from the prior step's checkpoint.
func TestResumeReRunsWholeParallelGroup(t *testing.T) {
	root := t.TempDir()
	p := runtime.Pipeline{
		Name:  "ParResume",
		Steps: []string{"Pre", "A", "B", "Post"},
		Stages: []runtime.Stage{
			{Name: "Pre", Steps: []string{"Pre"}},
			{Name: "G", Steps: []string{"A", "B"}, Parallel: true},
			{Name: "Post", Steps: []string{"Post"}},
		},
		Checkpoint: runtime.CheckpointConfig{Enabled: true, Strategy: "per_step"},
	}

	crash := true
	_, err := runtime.NewRunner(root).Run(p, nil, func(step string, ctx *runtime.RunContext) error {
		if step == "B" && crash {
			return errSimulatedCrash
		}
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected the simulated crash from branch B")
	}

	crash = false
	var mu sync.Mutex
	var executed []string
	res, err := runtime.NewRunner(root).Run(p, nil, func(step string, ctx *runtime.RunContext) error {
		mu.Lock()
		executed = append(executed, step)
		mu.Unlock()
		return nil
	}, true)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !res.Resumed {
		t.Error("expected Resumed = true")
	}
	sort.Strings(executed)
	if !equalStrings(executed, []string{"A", "B", "Post"}) {
		t.Fatalf("resume executed = %v, want the whole group (A, B) re-run then Post", executed)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
