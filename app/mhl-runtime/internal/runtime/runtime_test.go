package runtime_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/parser"
	"github.com/yanjustino/mhl-runtime/internal/runtime"
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

// AC-6: a checkpoint file is written under .mhl/state per step, and clearing on
// success removes it.
func TestCheckpointSaveWritesFileAndClearRemoves(t *testing.T) {
	root := t.TempDir()
	store := runtime.NewStore(root)

	cp := &runtime.Checkpoint{
		Pipeline:       "AutoFixPipeline",
		LastStep:       "AuditWithSkill",
		LastStepIndex:  0,
		CompletedSteps: []string{"AuditWithSkill"},
		Variables:      map[string]string{"issue_id": "BUG-102"},
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
		Pipeline:      "AutoFixPipeline",
		LastStep:      "AuditWithSkill",
		LastStepIndex: 0,
		TTLSeconds:    int64((1 * time.Hour).Seconds()),
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
	_, err := runner.Run(p, func(step string, ctx *runtime.RunContext) error {
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
	res, err := runner2.Run(p, func(step string, ctx *runtime.RunContext) error {
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

var errSimulatedCrash = &crashError{}

type crashError struct{}

func (*crashError) Error() string { return "simulated crash" }
