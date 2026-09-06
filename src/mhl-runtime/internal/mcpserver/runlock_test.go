package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// fakeLockingKV is fakeKV plus real PutIfAbsent / CompareAndSwap semantics
// (all under the embedded mutex), for exercising runLock without a child
// process.
type fakeLockingKV struct {
	*fakeKV
}

func newFakeLockingKV() *fakeLockingKV { return &fakeLockingKV{fakeKV: newFakeKV()} }

func (f *fakeLockingKV) CASCapable() bool { return true }

func (f *fakeLockingKV) PutIfAbsent(_ context.Context, key string, value any) (bool, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.m[key]; exists {
		return false, nil
	}
	f.m[key] = b
	return true, nil
}

func (f *fakeLockingKV) CompareAndSwap(_ context.Context, key string, expected []byte, newValue any) (bool, error) {
	b, err := json.Marshal(newValue)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, exists := f.m[key]
	if !exists || !bytes.Equal(cur, expected) {
		return false, nil
	}
	f.m[key] = b
	return true, nil
}

func newTestLock(kv LockingKVStore, replicaID string, clock *time.Time) *runLock {
	return &runLock{kv: kv, replicaID: replicaID, now: func() time.Time { return *clock }}
}

func TestRunLockAcquireConflictAndRelease(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "replica-A", &now)
	b := newTestLock(kv, "replica-B", &now)

	held, holder, err := a.acquire(context.Background(), "r1")
	if err != nil || !held || holder != "replica-A" {
		t.Fatalf("A.acquire = held:%v holder:%q err:%v", held, holder, err)
	}

	held, holder, err = b.acquire(context.Background(), "r1")
	if err != nil || held {
		t.Fatalf("B.acquire should fail while A holds a fresh lease: held:%v err:%v", held, err)
	}
	if holder != "replica-A" {
		t.Fatalf("B.acquire reported holder %q, want replica-A", holder)
	}

	if err := a.release(context.Background(), "r1"); err != nil {
		t.Fatalf("A.release: %v", err)
	}
	held, _, err = b.acquire(context.Background(), "r1")
	if err != nil || !held {
		t.Fatalf("B.acquire after release: held:%v err:%v", held, err)
	}
}

func TestRunLockTakeoverAfterExpiry(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "replica-A", &now)
	b := newTestLock(kv, "replica-B", &now)

	if held, _, _ := a.acquire(context.Background(), "r1"); !held {
		t.Fatal("A failed initial acquire")
	}

	// Advance past the TTL — A's lease is stale.
	now = now.Add(runLockTTL + time.Second)

	held, _, err := b.acquire(context.Background(), "r1")
	if err != nil || !held {
		t.Fatalf("B should take over an expired lease: held:%v err:%v", held, err)
	}

	// A's heartbeat now finds the lease is no longer its own.
	stillMine, err := a.renew(context.Background(), "r1")
	if err != nil || stillMine {
		t.Fatalf("A.renew after B took over = stillMine:%v err:%v", stillMine, err)
	}
	// B's heartbeat keeps working.
	stillMine, err = b.renew(context.Background(), "r1")
	if err != nil || !stillMine {
		t.Fatalf("B.renew = stillMine:%v err:%v", stillMine, err)
	}
}

func TestRunLockPeek(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "replica-A", &now)

	if st, _ := a.peek(context.Background(), "r1"); st != lockAbsent {
		t.Fatalf("peek before acquire = %v, want lockAbsent", st)
	}
	a.acquire(context.Background(), "r1")
	st, holder := a.peek(context.Background(), "r1")
	if st != lockFresh || holder != "replica-A" {
		t.Fatalf("peek after acquire = %v/%q, want lockFresh/replica-A", st, holder)
	}
	now = now.Add(runLockTTL + time.Second)
	if st, _ := a.peek(context.Background(), "r1"); st != lockExpired {
		t.Fatalf("peek after TTL = %v, want lockExpired", st)
	}
}

// Integration: two httpServer over one shared cas-capable store. A resume on B
// is refused while A holds a fresh run lock, and proceeds once A releases it.
func TestRunResumeRefusedWhileLockedElsewhere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline W {\n  step S1 { log(\"a\") }\n  step S2 { log(\"b\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kv := newFakeLockingKV()
	newSrv := func() *httpServer {
		_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
		if err != nil {
			t.Fatalf("buildHTTP: %v", err)
		}
		t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })
		return h
	}
	hA, hB := newSrv(), newSrv()
	if hA.lock == nil || hB.lock == nil {
		t.Fatal("run lock not enabled with a cas-capable store")
	}

	const runID = "run-x"
	ctx := context.Background()
	// Seed a resumable checkpoint + live status in the shared store.
	_ = kv.Put(ctx, runKey(runID, "checkpoint/W"), &runtime.Checkpoint{
		Pipeline: "W", NextStep: "S2", CompletedSteps: []string{"S1"}, Variables: map[string]any{},
	})
	_ = kv.Put(ctx, runKey(runID, "status"), RunStatusRec{
		Tool: "W", State: "working", StartedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// A is executing it — take the lock as execRun would.
	if held, _, _ := hA.lock.acquire(ctx, runID); !held {
		t.Fatal("A failed to acquire the run lock")
	}

	resume := func(h *httpServer) *rpcMsg {
		params, _ := json.Marshal(map[string]any{"runId": runID})
		return h.runResume(&session{}, rpcMsg{ID: json.RawMessage("1"), Params: params})
	}

	reply := resume(hB)
	if reply.Error == nil || !strings.Contains(reply.Error.Message, "another replica") {
		t.Fatalf("B.runResume should be refused while A holds the lock, got: %+v", reply)
	}

	// A finishes / dies — release the lock.
	_ = hA.lock.release(ctx, runID)

	reply = resume(hB)
	if reply.Error != nil {
		t.Fatalf("B.runResume after A released the lock should proceed, got error: %+v", reply.Error)
	}
}

// A store without the cas capability leaves run locking off — buildHTTP must
// not build a runLock, and behaviour is exactly as before.
func TestRunLockDisabledWithoutCAS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline W {\n  step S { log(\"x\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// plain fakeKV: no PutIfAbsent/CompareAndSwap ⇒ not a LockingKVStore.
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: newFakeKV()}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })
	if h.lock != nil {
		t.Fatal("run lock must be nil for a store without the cas capability")
	}
}
