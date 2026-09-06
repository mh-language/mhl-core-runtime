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

	leaseA, held, holder, err := a.acquire(context.Background(), "r1")
	if err != nil || !held || holder != "replica-A" || !leaseA.held() {
		t.Fatalf("A.acquire = held:%v holder:%q err:%v", held, holder, err)
	}

	_, held, holder, err = b.acquire(context.Background(), "r1")
	if err != nil || held {
		t.Fatalf("B.acquire should fail while A holds a fresh lease: held:%v err:%v", held, err)
	}
	if holder != "replica-A" {
		t.Fatalf("B.acquire reported holder %q, want replica-A", holder)
	}

	if err := a.release(context.Background(), leaseA); err != nil {
		t.Fatalf("A.release: %v", err)
	}
	_, held, _, err = b.acquire(context.Background(), "r1")
	if err != nil || !held {
		t.Fatalf("B.acquire after release: held:%v err:%v", held, err)
	}
}

func TestRunLockTakeoverAfterExpiry(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "replica-A", &now)
	b := newTestLock(kv, "replica-B", &now)

	leaseA, held, _, _ := a.acquire(context.Background(), "r1")
	if !held {
		t.Fatal("A failed initial acquire")
	}

	// Advance past the TTL — A's lease is stale.
	now = now.Add(runLockTTL + time.Second)

	leaseB, held, _, err := b.acquire(context.Background(), "r1")
	if err != nil || !held {
		t.Fatalf("B should take over an expired lease: held:%v err:%v", held, err)
	}

	// A's heartbeat now finds the lease is no longer its own.
	stillMine, err := a.renew(context.Background(), leaseA)
	if err != nil || stillMine {
		t.Fatalf("A.renew after B took over = stillMine:%v err:%v", stillMine, err)
	}
	// B's heartbeat keeps working.
	stillMine, err = b.renew(context.Background(), leaseB)
	if err != nil || !stillMine {
		t.Fatalf("B.renew = stillMine:%v err:%v", stillMine, err)
	}
}

func TestRunLockPeek(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "replica-A", &now)

	if st, _, _ := a.peek(context.Background(), "r1"); st != lockAbsent {
		t.Fatalf("peek before acquire = %v, want lockAbsent", st)
	}
	a.acquire(context.Background(), "r1")
	st, holder, err := a.peek(context.Background(), "r1")
	if err != nil || st != lockFresh || holder != "replica-A" {
		t.Fatalf("peek after acquire = %v/%q err:%v, want lockFresh/replica-A", st, holder, err)
	}
	now = now.Add(runLockTTL + time.Second)
	if st, _, _ := a.peek(context.Background(), "r1"); st != lockExpired {
		t.Fatalf("peek after TTL = %v, want lockExpired", st)
	}
}

// TestAssessmentExpiredHolderCannotDeleteSuccessor is the review probe (R5):
// A acquires, its lease lapses, B takes over, A releases late — B's live lease
// must survive and C must still be blocked.
func TestAssessmentExpiredHolderCannotDeleteSuccessor(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "A", &now)
	b := newTestLock(kv, "B", &now)
	c := newTestLock(kv, "C", &now)
	ctx := context.Background()

	leaseA, held, _, _ := a.acquire(ctx, "review")
	if !held {
		t.Fatal("A did not acquire")
	}
	now = now.Add(runLockTTL + time.Second)
	leaseB, held, _, _ := b.acquire(ctx, "review")
	if !held {
		t.Fatal("B did not take over the expired lease")
	}
	if err := a.release(ctx, leaseA); err != nil { // late release by the deposed holder
		t.Fatalf("A.release: %v", err)
	}
	if _, held, _, _ := c.acquire(ctx, "review"); held {
		t.Fatal("C acquired while B was still the live holder — A's late release wiped B's lease")
	}
	// B is still the holder and can renew.
	if mine, err := b.renew(ctx, leaseB); err != nil || !mine {
		t.Fatalf("B.renew after A's late release = mine:%v err:%v", mine, err)
	}

	// A may acquire again once B is done — a fresh acquisition, new token.
	if err := b.release(ctx, leaseB); err != nil {
		t.Fatalf("B.release: %v", err)
	}
	if _, held, _, _ := a.acquire(ctx, "review"); !held {
		t.Fatal("A could not re-acquire after B released")
	}
	if mine, _ := b.renew(ctx, leaseB); mine {
		t.Fatal("B.renew succeeded against A's new acquisition")
	}
}

// TestRunLockReleaseIsConditional: a release by a replica that no longer holds
// the lease is a no-op, and its own stale token does not let it renew.
func TestRunLockReleaseIsConditional(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "A", &now)
	b := newTestLock(kv, "B", &now)
	ctx := context.Background()

	leaseA, _, _, _ := a.acquire(ctx, "r1")
	now = now.Add(runLockTTL + time.Second)
	b.acquire(ctx, "r1") // takeover

	// A still holds its own acquisition handle but must not renew or release
	// B's lease.
	if mine, _ := a.renew(ctx, leaseA); mine {
		t.Fatal("A.renew succeeded after B took over")
	}
	if err := a.release(ctx, leaseA); err != nil {
		t.Fatalf("A.release: %v", err)
	}
	if st, holder, _ := b.peek(ctx, "r1"); st != lockFresh || holder != "B" {
		t.Fatalf("after A's late release, lease = %v/%q, want lockFresh/B", st, holder)
	}
}

// R5: a deposed holder's release, run late with the handle from its *first*
// acquisition, must not touch the lease of its own *second* acquisition (nor
// any successor's). The handle — not a per-runID map — is what scopes it.
func TestRunLockStaleHandleCannotReleaseLaterAcquisition(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "A", &now)
	ctx := context.Background()

	lease1, held, _, _ := a.acquire(ctx, "r1")
	if !held {
		t.Fatal("first acquire failed")
	}
	if err := a.release(ctx, lease1); err != nil {
		t.Fatalf("release lease1: %v", err)
	}
	lease2, held, _, _ := a.acquire(ctx, "r1")
	if !held || lease2.token == lease1.token {
		t.Fatalf("re-acquire: held=%v token reused=%v", held, lease2.token == lease1.token)
	}

	// The late, stale release of lease1 must be a no-op against lease2.
	if err := a.release(ctx, lease1); err != nil {
		t.Fatalf("stale release: %v", err)
	}
	if st, holder, _ := a.peek(ctx, "r1"); st != lockFresh || holder != "A" {
		t.Fatalf("lease2 after stale release of lease1 = %v/%q, want lockFresh/A", st, holder)
	}
	if mine, _ := a.renew(ctx, lease2); !mine {
		t.Fatal("lease2 renew failed after a stale release of lease1")
	}
}

// ownsLease is the checkpoint-write fence: true only for the exact live
// acquisition, false after takeover or expiry.
func TestRunLockOwnsLease(t *testing.T) {
	kv := newFakeLockingKV()
	now := time.Now()
	a := newTestLock(kv, "A", &now)
	b := newTestLock(kv, "B", &now)
	ctx := context.Background()

	leaseA, held, _, _ := a.acquire(ctx, "r1")
	if !held {
		t.Fatal("A acquire failed")
	}
	if ok, err := a.ownsLease(ctx, leaseA); err != nil || !ok {
		t.Fatalf("A.ownsLease while holding = %v err:%v", ok, err)
	}
	// B has no lease here.
	if ok, _ := b.ownsLease(ctx, leaseHandle{runID: "r1", token: "nope"}); ok {
		t.Fatal("B.ownsLease true for a bogus handle")
	}
	// Takeover: A's handle no longer owns.
	now = now.Add(runLockTTL + time.Second)
	if _, held, _, _ := b.acquire(ctx, "r1"); !held {
		t.Fatal("B takeover failed")
	}
	if ok, _ := a.ownsLease(ctx, leaseA); ok {
		t.Fatal("A.ownsLease still true after B took over")
	}
}

// R4: renew is bounded by its context — a wedged store cannot make the
// heartbeat loop block past the point a takeover could begin.
func TestRunLockRenewRespectsContextDeadline(t *testing.T) {
	kv := &blockGetKV{fakeLockingKV: newFakeLockingKV()}
	now := time.Now()
	l := newTestLock(kv, "A", &now)
	lease, held, _, _ := l.acquire(context.Background(), "r1") // PutIfAbsent path, no Get
	if !held {
		t.Fatal("acquire failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := l.renew(ctx, lease)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("renew against a wedged store returned nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("renew did not return within the context deadline — it is not bounded")
	}
}

// blockGetKV hangs every Get until the caller's context is done.
type blockGetKV struct{ *fakeLockingKV }

func (k *blockGetKV) Get(ctx context.Context, _ string) ([]byte, bool, error) {
	<-ctx.Done()
	return nil, false, ctx.Err()
}

// TestPeekReportsStoreError: a store failure surfaces as lockUnknown + err, not
// as "no lease".
func TestPeekReportsStoreError(t *testing.T) {
	kv := &errGetKV{fakeLockingKV: newFakeLockingKV()}
	now := time.Now()
	l := newTestLock(kv, "A", &now)
	st, _, err := l.peek(context.Background(), "r1")
	if err == nil || st != lockUnknown {
		t.Fatalf("peek with a failing store = %v err:%v, want lockUnknown + error", st, err)
	}
}

func TestHeartbeatDecision(t *testing.T) {
	safe := runLockTTL - runLockHeartbeat
	errStore := errTest("store down")
	cases := []struct {
		name  string
		mine  bool
		err   error
		since time.Duration
		want  hbAction
	}{
		{"renewed", true, nil, time.Second, hbRefreshed},
		{"lost to another replica", false, nil, time.Second, hbCancel},
		{"transient error, lease still safe", false, errStore, safe - time.Second, hbContinue},
		{"error past the safe window", false, errStore, safe + time.Second, hbCancel},
		{"error exactly at the window", false, errStore, safe, hbCancel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := heartbeatDecision(tc.mine, tc.err, tc.since); got != tc.want {
				t.Fatalf("heartbeatDecision(%v,%v,%v) = %v, want %v", tc.mine, tc.err, tc.since, got, tc.want)
			}
		})
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// errGetKV fails every Get; PutIfAbsent/CompareAndSwap still work.
type errGetKV struct{ *fakeLockingKV }

func (k *errGetKV) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, errTest("synthetic store Get failure")
}

// assessmentBrokenCAS is cas-capable but every acquire attempt errors.
type assessmentBrokenCAS struct{ *fakeLockingKV }

func (k *assessmentBrokenCAS) PutIfAbsent(context.Context, string, any) (bool, error) {
	return false, errTest("synthetic store unavailable for acquire")
}

// TestAssessmentAcquireErrorMustNotExecute is the review probe (R4): a run
// whose execution lease cannot be acquired must not run a single step.
func TestAssessmentAcquireErrorMustNotExecute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline W {\n  step Work { log(\"EXECUTED\") }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kv := &assessmentBrokenCAS{newFakeLockingKV()}
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP: %v", err)
	}
	t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rn := &asyncRun{
		id: "review", tool: h.srv.tools["W"], args: map[string]any{},
		state: "working", started: time.Now(), updated: time.Now(),
		cancel: cancel, done: make(chan struct{}), logs: newRingLog(),
	}
	h.runs.Put(rn)
	h.execRun(ctx, rn, false)

	rn.mu.Lock()
	state, msg := rn.state, rn.errMsg
	rn.mu.Unlock()
	if state == "completed" {
		t.Fatal("workflow completed despite failing to acquire the execution lease")
	}
	if state != "failed" || !strings.Contains(msg, "lease") {
		t.Fatalf("run state = %q errMsg = %q, want failed + a lease error", state, msg)
	}
}

// TestBuildHTTPRefusesNonCASStoreWithoutSingleReplica: a Store that cannot
// coordinate is a startup error unless the operator sets SingleReplica.
func TestBuildHTTPRefusesNonCASStoreWithoutSingleReplica(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline W {\n  step S { log(\"x\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: newFakeKV()}, io.Discard); err == nil {
		t.Fatal("buildHTTP accepted a non-cas Store without SingleReplica")
	}
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: newFakeKV(), SingleReplica: true}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP with SingleReplica: %v", err)
	}
	t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })
	if h.lock != nil {
		t.Fatal("run lock must be nil for a non-cas Store")
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
	leaseA, held, _, _ := hA.lock.acquire(ctx, runID)
	if !held {
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
	_ = hA.lock.release(ctx, leaseA)

	reply = resume(hB)
	if reply.Error != nil {
		t.Fatalf("B.runResume after A released the lock should proceed, got error: %+v", reply.Error)
	}
}

// A cas-capable store enables the run lock; TestBuildHTTPRefusesNonCASStore...
// covers the non-cas Store paths (error without SingleReplica, lock nil with it).
func TestRunLockEnabledWithCAS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline W {\n  step S { log(\"x\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: newFakeLockingKV()}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })
	if h.lock == nil {
		t.Fatal("run lock must be enabled for a cas-capable store")
	}
}
