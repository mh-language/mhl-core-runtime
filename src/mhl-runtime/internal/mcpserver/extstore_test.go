package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// A fenced extStateStore fails mutating writes once the lease is lost, and
// leaves earlier checkpoints untouched — both on the check-then-write path and
// the atomic store-side path.
func TestExtStateStoreFencesWritesOnLostLease(t *testing.T) {
	cp := &runtime.Checkpoint{Pipeline: "P", NextStep: "S2", Variables: map[string]any{}}

	t.Run("check-then-write", func(t *testing.T) {
		kv := newFakeKV()
		lost := false
		ss := newExtStateStore(kv, "r1", &stateFence{check: func() error {
			if lost {
				return ErrLeaseLost
			}
			return nil
		}})
		if err := ss.Save(cp); err != nil {
			t.Fatalf("Save while lease held: %v", err)
		}
		lost = true
		if err := ss.Save(cp); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("Save after lease lost = %v, want ErrLeaseLost", err)
		}
		if err := ss.Clear("P"); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("Clear after lease lost = %v, want ErrLeaseLost", err)
		}
		if _, found, _ := ss.Load("P"); !found {
			t.Error("checkpoint from the pre-loss Save was lost")
		}
	})

	t.Run("atomic store-side", func(t *testing.T) {
		fw := &fakeFencedWriter{fakeKV: newFakeKV(), ownsLease: true}
		ss := newExtStateStore(fw, "r1", &stateFence{fw: fw, lockKey: "run/r1/lock", holder: "A", token: "t1"})
		if err := ss.Save(cp); err != nil {
			t.Fatalf("Save while lease held: %v", err)
		}
		fw.ownsLease = false
		if err := ss.Save(cp); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("Save after takeover = %v, want ErrLeaseLost", err)
		}
		if err := ss.Clear("P"); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("Clear after takeover = %v, want ErrLeaseLost", err)
		}
	})
}

// fakeFencedWriter is a FencedWriter whose lease ownership is toggled directly.
type fakeFencedWriter struct {
	*fakeKV
	ownsLease bool
}

func (f *fakeFencedWriter) FenceCapable() bool { return true }

func (f *fakeFencedWriter) PutFenced(ctx context.Context, key string, value any, _, _, _ string) (bool, error) {
	if !f.ownsLease {
		return false, nil
	}
	return true, f.Put(ctx, key, value)
}

func (f *fakeFencedWriter) DeleteFenced(ctx context.Context, key, _, _, _ string) (bool, error) {
	if !f.ownsLease {
		return false, nil
	}
	return true, f.Delete(ctx, key)
}

// fakeKV is an in-process KVStore for exercising the ext* adapters without a
// child process.
type fakeKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newFakeKV() *fakeKV { return &fakeKV{m: map[string][]byte{}} }

func (f *fakeKV) Get(_ context.Context, key string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.m[key]
	return b, ok, nil
}

func (f *fakeKV) Put(_ context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = b
	return nil
}

func (f *fakeKV) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, key)
	return nil
}

func (f *fakeKV) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func TestExtSessionStore(t *testing.T) {
	kv := newFakeKV()
	s := newExtSessionStore(kv)

	s.Put(&session{id: "a", principal: "alice", initialized: true, protocol: "2025-06-18"})
	s.Put(&session{id: "b"})
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}

	got, ok := s.Get("a")
	if !ok || got.principal != "alice" || !got.initialized || got.protocol != "2025-06-18" {
		t.Fatalf("Get(a) = %+v ok=%v", got, ok)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get(nope) should miss")
	}

	if !s.Delete("a") || s.Delete("a") {
		t.Error("Delete should report presence exactly once")
	}

	// Age "b" and sweep.
	var rec sessionRec
	raw, _, _ := kv.Get(context.Background(), "session/b")
	_ = json.Unmarshal(raw, &rec)
	rec.LastUsed = time.Now().Add(-2 * time.Hour)
	_ = kv.Put(context.Background(), "session/b", rec)
	s.SweepIdle(time.Hour)
	if s.Len() != 0 {
		t.Errorf("Len after sweep = %d, want 0", s.Len())
	}
}

func TestExtCheckpointStoreAndStateStore(t *testing.T) {
	kv := newFakeKV()
	cps, err := newExtCheckpointStore(kv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cps.Close() })

	const runID = "r1"
	if cps.Exists(runID) {
		t.Fatal("Exists on empty store")
	}
	if _, ok := cps.ReadOwner(runID); ok {
		t.Fatal("ReadOwner on empty store")
	}

	// The runtime.StateStore leg writes the checkpoint...
	ss := newExtStateStore(kv, runID, nil)
	cp := &runtime.Checkpoint{Pipeline: "P", NextStep: "Two", CompletedSteps: []string{"One"},
		Variables: map[string]any{"x": 1.0}}
	if err := ss.Save(cp); err != nil {
		t.Fatal(err)
	}
	// ...and the CheckpointStore leg (reconstruct path) sees it.
	if !cps.Exists(runID) {
		t.Error("Exists false after Save")
	}
	got, ok := cps.Load(runID)
	if !ok || got.Pipeline != "P" || got.NextStep != "Two" {
		t.Errorf("Load = %+v ok=%v", got, ok)
	}
	back, ok, err := ss.Load("P")
	if err != nil || !ok || back.Variables["x"] != 1.0 {
		t.Errorf("StateStore.Load = %+v ok=%v err=%v", back, ok, err)
	}

	if err := cps.WriteOwner(runID, Owner("owner-hash")); err != nil {
		t.Fatal(err)
	}
	if o, ok := cps.ReadOwner(runID); !ok || o != "owner-hash" {
		t.Errorf("ReadOwner = %q ok=%v", o, ok)
	}

	_ = ss.WriteResult("P", map[string]any{"done": true})
	if err := cps.Remove(runID); err != nil {
		t.Fatal(err)
	}
	if cps.Exists(runID) {
		t.Error("Remove left checkpoint keys")
	}
	if keys, _ := kv.List(context.Background(), "run/"+runID+"/"); len(keys) != 0 {
		t.Errorf("Remove left %d key(s): %v", len(keys), keys)
	}
}

// TestExtCheckpointStoreListStatuses: ListStatuses returns one entry per run
// that has a status key, and does not mistake an owner / checkpoint / cancel
// key for one.
func TestExtCheckpointStoreListStatuses(t *testing.T) {
	kv := newFakeKV()
	cps, _ := newExtCheckpointStore(kv)
	t.Cleanup(func() { _ = cps.Close() })

	if m, err := cps.ListStatuses(); err != nil || len(m) != 0 {
		t.Fatalf("empty store: ListStatuses = %v, %v", m, err)
	}

	now := time.Now()
	_ = cps.WriteStatus("r1", RunStatusRec{Tool: "A", State: "completed", UpdatedAt: now})
	_ = cps.WriteStatus("r2", RunStatusRec{Tool: "B", State: "working", UpdatedAt: now})
	_ = cps.WriteOwner("r1", Owner("o"))
	_ = kv.Put(context.Background(), runKey("r2", "checkpoint/W"), map[string]any{})
	_ = cps.RequestCancel("r2")

	m, err := cps.ListStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m["r1"].Tool != "A" || m["r2"].State != "working" {
		t.Fatalf("ListStatuses = %+v", m)
	}
}

// scanKV is a fakeKV that also implements the optional statusScanner fast path,
// counting how often it is used so a test can prove the adapter prefers it.
type scanKV struct {
	*fakeKV
	listCalls, countCalls int
}

func (s *scanKV) statusRecs() map[string]RunStatusRec {
	s.fakeKV.mu.Lock()
	defer s.fakeKV.mu.Unlock()
	out := map[string]RunStatusRec{}
	for k, raw := range s.fakeKV.m {
		id, ok := strings.CutSuffix(strings.TrimPrefix(k, "run/"), "/status")
		if !ok || strings.Contains(id, "/") {
			continue
		}
		var rec RunStatusRec
		if json.Unmarshal(raw, &rec) == nil {
			out[id] = rec
		}
	}
	return out
}

func (s *scanKV) ListStatusRecs(context.Context) (map[string]RunStatusRec, bool, error) {
	s.listCalls++
	return s.statusRecs(), true, nil
}

func (s *scanKV) CountPendingRuns(context.Context) (int, bool, error) {
	s.countCalls++
	n := 0
	for _, rec := range s.statusRecs() {
		if rec.State == RunStatePending {
			n++
		}
	}
	return n, true, nil
}

// TestExtCheckpointStoreStatusScanFastPath: when the KV implements
// statusScanner, ListStatuses and CountPending route through it (one
// round-trip) instead of the list-plus-get fallback.
func TestExtCheckpointStoreStatusScanFastPath(t *testing.T) {
	kv := &scanKV{fakeKV: newFakeKV()}
	cps, _ := newExtCheckpointStore(kv)
	t.Cleanup(func() { _ = cps.Close() })

	now := time.Now()
	_ = cps.WriteStatus("r1", RunStatusRec{Tool: "A", State: RunStatePending, StartedAt: now, UpdatedAt: now})
	_ = cps.WriteStatus("r2", RunStatusRec{Tool: "B", State: RunStateWorking, UpdatedAt: now})
	_ = cps.WriteStatus("r3", RunStatusRec{Tool: "C", State: RunStatePending, StartedAt: now, UpdatedAt: now})
	_ = cps.WriteOwner("r1", Owner("o"))

	m, err := cps.ListStatuses()
	if err != nil || len(m) != 3 {
		t.Fatalf("ListStatuses = %+v, %v", m, err)
	}
	if kv.listCalls != 1 {
		t.Fatalf("expected 1 scan call, got %d", kv.listCalls)
	}

	n, ok, err := cps.CountPending()
	if err != nil || !ok || n != 2 {
		t.Fatalf("CountPending = %d, ok=%v, err=%v (want 2, true, nil)", n, ok, err)
	}
	if kv.countCalls != 1 {
		t.Fatalf("expected 1 count call, got %d", kv.countCalls)
	}
}

// The extension store is Shared, and its status record / cancel flag
// round-trip through the KV without tripping Exists/Load.
func TestExtCheckpointStoreLiveStatusAndCancel(t *testing.T) {
	kv := newFakeKV()
	cps, _ := newExtCheckpointStore(kv)
	t.Cleanup(func() { _ = cps.Close() })

	if !cps.Shared() {
		t.Error("extension store must report Shared() == true")
	}
	if _, ok := cps.ReadStatus("r1"); ok || cps.CancelRequested("r1") {
		t.Fatal("empty store should have no status / cancel for r1")
	}

	rec := RunStatusRec{Tool: "P", State: "working", Step: "Two", Reached: []string{"One", "Two"}}
	if err := cps.WriteStatus("r1", rec); err != nil {
		t.Fatal(err)
	}
	got, ok := cps.ReadStatus("r1")
	if !ok || got.State != "working" || got.Step != "Two" {
		t.Fatalf("ReadStatus = %+v ok=%v", got, ok)
	}
	if cps.Exists("r1") {
		t.Error("a status key must not make Exists() true")
	}

	if err := cps.RequestCancel("r1"); err != nil {
		t.Fatal(err)
	}
	if !cps.CancelRequested("r1") {
		t.Fatal("CancelRequested should be true after RequestCancel")
	}
	if err := cps.Remove("r1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cps.ReadStatus("r1"); ok || cps.CancelRequested("r1") {
		t.Error("Remove should drop the status key and the cancel flag")
	}
}

// A "" owner is a no-op (nothing persisted) — matches the disk store.
func TestExtCheckpointStoreOwnerEmpty(t *testing.T) {
	kv := newFakeKV()
	cps, _ := newExtCheckpointStore(kv)
	t.Cleanup(func() { _ = cps.Close() })
	if err := cps.WriteOwner("r", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := cps.ReadOwner("r"); ok {
		t.Error("empty owner should not be readable")
	}
}
