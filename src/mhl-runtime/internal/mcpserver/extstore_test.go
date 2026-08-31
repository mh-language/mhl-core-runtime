package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

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
	ss := newExtStateStore(kv, runID)
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
