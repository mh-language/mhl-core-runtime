package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// fakeStateStore is an in-memory runtime.StateStore — the proof that a Runner
// can be driven entirely off an alternative checkpoint backend (the seam the
// serve layer uses in Phase 3), touching no files.
type fakeStateStore struct {
	mu      sync.Mutex
	cps     map[string]*runtime.Checkpoint
	results map[string]map[string]any
	saves   int
	clears  int
}

func newFakeStateStore() *fakeStateStore {
	return &fakeStateStore{
		cps:     map[string]*runtime.Checkpoint{},
		results: map[string]map[string]any{},
	}
}

func (f *fakeStateStore) Load(pipeline string) (*runtime.Checkpoint, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp, ok := f.cps[pipeline]
	return cp, ok, nil
}

func (f *fakeStateStore) Save(cp *runtime.Checkpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := *cp
	f.cps[cp.Pipeline] = &clone
	f.saves++
	return nil
}

func (f *fakeStateStore) Clear(pipeline string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cps, pipeline)
	f.clears++
	return nil
}

func (f *fakeStateStore) WriteResult(pipeline string, vars map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[pipeline] = vars
	return nil
}

// TestWithStateStoreCompletes drives a checkpointed pipeline to completion
// through the fake and asserts nothing was written under the on-disk state
// dir — the built-in Store was never consulted.
func TestWithStateStoreCompletes(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)
	fake := newFakeStateStore()

	var executed []string
	runner := runtime.NewRunner(root).WithStateStore(fake)
	res, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		executed = append(executed, step)
		return nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(executed) != 2 || executed[0] != "AuditWithSkill" || executed[1] != "RefinementLoop" {
		t.Fatalf("executed = %v", executed)
	}
	if fake.saves == 0 {
		t.Error("expected per-step Save calls on the fake store")
	}
	if fake.clears != 1 {
		t.Errorf("expected exactly one Clear on completion, got %d", fake.clears)
	}
	if _, ok, _ := fake.Load(p.Name); ok {
		t.Error("checkpoint should be cleared after a clean completion")
	}
	if _, err := os.Stat(filepath.Join(root, runtime.StateDirName)); !os.IsNotExist(err) {
		t.Errorf("built-in state dir was written despite WithStateStore: err=%v", err)
	}
	_ = res
}

// TestWithStateStoreResume crashes mid-pipeline against the fake, then resumes
// a fresh Runner pointed at the same fake and asserts the completed step is
// not re-executed.
func TestWithStateStoreResume(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)
	fake := newFakeStateStore()

	_, err := runtime.NewRunner(root).WithStateStore(fake).Run(
		context.Background(), p, nil,
		func(_ context.Context, step string, ctx *runtime.RunContext) error {
			if step == "RefinementLoop" {
				return errSimulatedCrash
			}
			ctx.Vars["done_"+step] = true
			return nil
		}, false)
	if err == nil {
		t.Fatal("expected the simulated crash")
	}
	if _, ok, _ := fake.Load(p.Name); !ok {
		t.Fatal("expected a checkpoint in the fake store after the crash")
	}

	var resumeSteps []string
	res, err := runtime.NewRunner(root).WithStateStore(fake).Run(
		context.Background(), p, nil,
		func(_ context.Context, step string, ctx *runtime.RunContext) error {
			resumeSteps = append(resumeSteps, step)
			return nil
		}, true)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !res.Resumed {
		t.Error("expected Resumed = true")
	}
	if len(resumeSteps) != 1 || resumeSteps[0] != "RefinementLoop" {
		t.Fatalf("resume executed = %v, want [RefinementLoop]", resumeSteps)
	}
}
