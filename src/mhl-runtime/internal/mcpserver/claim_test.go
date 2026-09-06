package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// With durable intake active (a cas store), run/start returns `pending` and does
// not launch; the per-replica claim loop takes the pending run to completion.
func TestDurableIntakeClaimLoopRunsPendingRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline P {\n  input who: string\n  step S { log(who) }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kv := newFakeLockingKV()
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP: %v", err)
	}
	t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })

	sess := &session{id: "s1"}
	res := decodeResult(t, h.runStart(sess, mkMsg("run/start", map[string]any{
		"name": "P", "arguments": map[string]any{"who": "bob"},
	})))
	if res["state"] != string(RunStatePending) {
		t.Fatalf("run/start state = %v, want pending", res["state"])
	}
	runID := res["runId"].(string)
	if _, found, _ := kv.Get(context.Background(), intakeKey(runID)); !found {
		t.Fatal("no intake record after run/start")
	}

	// The claim loop (nudged by run/start) drives it to a terminal state.
	var st map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st = decodeResult(t, h.runStatus(sess, mkMsg("run/status", map[string]any{"runId": runID})))
		if s, _ := st["state"].(string); s == string(RunStateCompleted) || s == string(RunStateFailed) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st["state"] != string(RunStateCompleted) {
		t.Fatalf("pending run never completed via the claim loop: %v", st)
	}
}

func seedStatus(t *testing.T, kv *fakeLockingKV, id string, rec RunStatusRec) {
	t.Helper()
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	kv.mu.Lock()
	kv.m[runKey(id, "status")] = b
	kv.mu.Unlock()
}

func TestClaimNextCASPicksOldestPendingAndFlipsIt(t *testing.T) {
	kv := newFakeLockingKV()
	base := time.Now()
	seedStatus(t, kv, "r-working", RunStatusRec{Tool: "P", State: RunStateWorking, StartedAt: base})
	seedStatus(t, kv, "r-new", RunStatusRec{Tool: "P", State: RunStatePending, StartedAt: base.Add(2 * time.Minute)})
	seedStatus(t, kv, "r-old", RunStatusRec{Tool: "P", State: RunStatePending, StartedAt: base.Add(1 * time.Minute)})

	clk := base.Add(5 * time.Minute)
	id, rec, ok, err := claimNextCAS(context.Background(), kv, "replica-A", func() time.Time { return clk })
	if err != nil || !ok {
		t.Fatalf("claim 1: ok=%v err=%v", ok, err)
	}
	if id != "r-old" {
		t.Errorf("claimed %q, want the older pending r-old", id)
	}
	if rec.State != RunStateClaimed || rec.Holder != "replica-A" || !rec.UpdatedAt.Equal(clk) {
		t.Errorf("claimed rec = %+v", rec)
	}
	// It is persisted as claimed — no longer a claim candidate.
	raw, _, _ := kv.Get(context.Background(), runKey("r-old", "status"))
	var back RunStatusRec
	_ = json.Unmarshal(raw, &back)
	if back.State != RunStateClaimed || back.Holder != "replica-A" {
		t.Errorf("persisted rec = %+v, want claimed by replica-A", back)
	}

	// Second claim takes the remaining pending; third finds none.
	id, _, ok, err = claimNextCAS(context.Background(), kv, "replica-B", func() time.Time { return clk })
	if err != nil || !ok || id != "r-new" {
		t.Fatalf("claim 2: id=%q ok=%v err=%v", id, ok, err)
	}
	id, _, ok, err = claimNextCAS(context.Background(), kv, "replica-B", func() time.Time { return clk })
	if err != nil || ok || id != "" {
		t.Fatalf("claim 3: id=%q ok=%v err=%v — want no pending run", id, ok, err)
	}
}

func TestClaimNextCASSkipsCandidateLostToARace(t *testing.T) {
	kv := newFakeLockingKV()
	base := time.Now()
	seedStatus(t, kv, "r1", RunStatusRec{Tool: "P", State: RunStatePending, StartedAt: base.Add(1 * time.Minute)})
	seedStatus(t, kv, "r2", RunStatusRec{Tool: "P", State: RunStatePending, StartedAt: base.Add(2 * time.Minute)})

	// raceKV rewrites r1's status between the List/Get snapshot and the CAS, so
	// the CompareAndSwap on r1 misses and the scan must fall through to r2.
	rk := &raceRewriteKV{fakeLockingKV: kv, onKey: runKey("r1", "status"), rewrite: RunStatusRec{
		Tool: "P", State: RunStatePending, StartedAt: base.Add(1 * time.Minute), Step: "moved",
	}}

	id, _, ok, err := claimNextCAS(context.Background(), rk, "replica-A", time.Now)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if id != "r2" {
		t.Errorf("claimed %q, want r2 (r1 was lost to a race)", id)
	}
}

// A stale non-terminal run (claimed/working/paused) whose lease is gone is
// returned to `pending`; a fresh one, or one with a live lease, is left alone.
func TestReconcileRunsReturnsOrphansToPending(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline P {\n  step S { var x = 1 }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kv := newFakeLockingKV()
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP: %v", err)
	}
	h.runsCancel() // stop the background claim loop; drive reconcile directly
	t.Cleanup(func() { _ = h.cps.Close() })
	ctx := context.Background()

	old := time.Now().Add(-3 * runLockTTL)
	// Stale + no lease → reclaimed, for each non-terminal state.
	seedStatus(t, kv, "orphan-claimed", RunStatusRec{Tool: "P", State: RunStateClaimed, Holder: "dead", StartedAt: old, UpdatedAt: old})
	seedStatus(t, kv, "orphan-working", RunStatusRec{Tool: "P", State: RunStateWorking, Holder: "dead", StartedAt: old, UpdatedAt: old})
	seedStatus(t, kv, "orphan-paused", RunStatusRec{Tool: "P", State: RunStatePaused, Holder: "dead", StartedAt: old, UpdatedAt: old})
	// Left alone: fresh status, terminal, and stale-but-lease-held.
	seedStatus(t, kv, "fresh", RunStatusRec{Tool: "P", State: RunStateWorking, Holder: "r1", StartedAt: time.Now(), UpdatedAt: time.Now()})
	seedStatus(t, kv, "done", RunStatusRec{Tool: "P", State: RunStateCompleted, Holder: "r1", StartedAt: old, UpdatedAt: old})
	seedStatus(t, kv, "held", RunStatusRec{Tool: "P", State: RunStateWorking, Holder: "r2", StartedAt: old, UpdatedAt: old})
	if _, held, _, _ := h.lock.acquire(ctx, "held"); !held {
		t.Fatal("could not seed a held lease")
	}

	h.reconcileRuns(ctx)

	state := func(id string) RunStatusRec {
		raw, _, _ := kv.Get(ctx, runKey(id, "status"))
		var r RunStatusRec
		_ = json.Unmarshal(raw, &r)
		return r
	}
	for _, id := range []string{"orphan-claimed", "orphan-working", "orphan-paused"} {
		if r := state(id); r.State != RunStatePending || r.Holder != "" {
			t.Errorf("%s = state:%q holder:%q, want pending with holder cleared", id, r.State, r.Holder)
		}
	}
	if r := state("fresh"); r.State != RunStateWorking {
		t.Errorf("fresh = %q, want left working (not yet stale)", r.State)
	}
	if r := state("done"); r.State != RunStateCompleted {
		t.Errorf("done = %q, want left completed (terminal)", r.State)
	}
	if r := state("held"); r.State != RunStateWorking {
		t.Errorf("held = %q, want left working (lease is live)", r.State)
	}
}

// Two replicas over one shared store: replica A accepts a run and dies with it
// claimed but never leased; replica B's reconcile returns it to `pending` and
// B's claim loop runs it to completion — A never executed it.
func TestDurableIntakeReplicaDeathRecoveredByPeer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline P {\n  input who: string\n  step S { log(who) }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kv := newFakeLockingKV()

	_, hA, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP A: %v", err)
	}
	// A is "dead" from the outset: its claim loop never runs, so run/start
	// still persists the durable records but A never claims or executes.
	hA.runsCancel()
	t.Cleanup(func() { _ = hA.cps.Close() })

	_, hB, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP B: %v", err)
	}
	t.Cleanup(func() { hB.runsCancel(); _ = hB.cps.Close() })

	sess := &session{id: "s1"}
	res := decodeResult(t, hA.runStart(sess, mkMsg("run/start", map[string]any{
		"name": "P", "arguments": map[string]any{"who": "bob"},
	})))
	runID, _ := res["runId"].(string)
	if runID == "" {
		t.Fatalf("run/start on A: %v", res)
	}

	// Pin the run as a stale `claimed` left by dead A (as if A had claimed it
	// and died before acquiring a lease).
	old := time.Now().Add(-3 * runLockTTL)
	seedStatus(t, kv, runID, RunStatusRec{
		Tool: "P", State: RunStateClaimed, Holder: "replica-A-dead",
		StartedAt: old, UpdatedAt: old,
	})

	// B reconciles the orphan back to pending, then drains it.
	hB.reconcileRuns(context.Background())
	hB.drainClaims(context.Background())

	var st map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st = decodeResult(t, hB.runStatus(sess, mkMsg("run/status", map[string]any{"runId": runID})))
		if s, _ := st["state"].(string); s == string(RunStateCompleted) || s == string(RunStateFailed) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st["state"] != string(RunStateCompleted) {
		t.Fatalf("run not completed on B: %v", st)
	}
	if pm, ok := hA.metrics.(*promMetrics); ok && pm.runsCompleted.Load() != 0 {
		t.Errorf("replica A completed %d runs, want 0", pm.runsCompleted.Load())
	}
	if pm, ok := hB.metrics.(*promMetrics); ok && pm.runsCompleted.Load() != 1 {
		t.Errorf("replica B completed %d runs, want 1", pm.runsCompleted.Load())
	}
}

// fakeClaimNexter is a native-claim backend for the dispatcher test.
type fakeClaimNexter struct {
	supported bool
	id        string
	calls     int
}

func (f *fakeClaimNexter) ClaimNext(_ context.Context, holder string) (string, RunStatusRec, bool, error) {
	f.calls++
	if !f.supported {
		return "", RunStatusRec{}, false, ErrClaimNextUnsupported
	}
	if f.id == "" {
		return "", RunStatusRec{}, false, nil
	}
	return f.id, RunStatusRec{Tool: "P", State: RunStateClaimed, Holder: holder}, true, nil
}

// claimNext prefers a native ClaimNexter; ErrClaimNextUnsupported from one makes
// it fall back to the generic CAS scan.
func TestClaimNextPrefersNativeThenFallsBack(t *testing.T) {
	ctx := context.Background()

	// Native, supported → used directly, no CAS scan.
	native := &fakeClaimNexter{supported: true, id: "n1"}
	kv := newFakeLockingKV()
	seedStatus(t, kv, "cas1", RunStatusRec{Tool: "P", State: RunStatePending, StartedAt: time.Now()})
	id, rec, ok, err := claimNext(ctx, native, kv, "A")
	if err != nil || !ok || id != "n1" || rec.Holder != "A" {
		t.Fatalf("native path: id=%q ok=%v holder=%q err=%v", id, ok, rec.Holder, err)
	}

	// Native, unsupported → falls back to the CAS scan over kv.
	unsup := &fakeClaimNexter{supported: false}
	id, _, ok, err = claimNext(ctx, unsup, kv, "A")
	if err != nil || !ok || id != "cas1" {
		t.Fatalf("fallback path: id=%q ok=%v err=%v (unsup.calls=%d)", id, ok, err, unsup.calls)
	}
	if unsup.calls != 1 {
		t.Errorf("unsupported ClaimNexter called %d times, want 1", unsup.calls)
	}
}

// raceRewriteKV mutates one key's stored bytes exactly once, the first time
// CompareAndSwap is called for it — simulating another replica winning the race.
type raceRewriteKV struct {
	*fakeLockingKV
	onKey   string
	rewrite RunStatusRec
	done    bool
}

func (k *raceRewriteKV) CompareAndSwap(ctx context.Context, key string, expected []byte, newValue any) (bool, error) {
	if key == k.onKey && !k.done {
		k.done = true
		b, _ := json.Marshal(k.rewrite)
		k.mu.Lock()
		k.m[key] = b
		k.mu.Unlock()
	}
	return k.fakeLockingKV.CompareAndSwap(ctx, key, expected, newValue)
}
