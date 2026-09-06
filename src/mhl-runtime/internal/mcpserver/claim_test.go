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

// A run stuck in `claimed` past claimReclaimAfter whose claimer left no lease is
// returned to `pending`; a fresh claim, or one with a live lease, is left alone.
func TestReconcileClaimsReturnsOrphanedClaimToPending(t *testing.T) {
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
	seedStatus(t, kv, "orphan", RunStatusRec{Tool: "P", State: RunStateClaimed, Holder: "dead", StartedAt: old, UpdatedAt: old})
	seedStatus(t, kv, "fresh", RunStatusRec{Tool: "P", State: RunStateClaimed, Holder: "r1", StartedAt: time.Now(), UpdatedAt: time.Now()})
	seedStatus(t, kv, "held", RunStatusRec{Tool: "P", State: RunStateClaimed, Holder: "r2", StartedAt: old, UpdatedAt: old})
	if _, held, _, _ := h.lock.acquire(ctx, "held"); !held {
		t.Fatal("could not seed a held lease")
	}

	h.reconcileClaims(ctx)

	state := func(id string) RunStatusRec {
		raw, _, _ := kv.Get(ctx, runKey(id, "status"))
		var r RunStatusRec
		_ = json.Unmarshal(raw, &r)
		return r
	}
	if r := state("orphan"); r.State != RunStatePending || r.Holder != "" {
		t.Errorf("orphan = state:%q holder:%q, want pending with holder cleared", r.State, r.Holder)
	}
	if r := state("fresh"); r.State != RunStateClaimed {
		t.Errorf("fresh = %q, want left claimed (not yet stale)", r.State)
	}
	if r := state("held"); r.State != RunStateClaimed {
		t.Errorf("held = %q, want left claimed (lease is live)", r.State)
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
