package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

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
