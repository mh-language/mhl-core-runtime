package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// resolveReplicaID names this process among a fleet sharing one store. An
// explicit MHL_SERVE_REPLICA_ID wins; otherwise it is hostname plus a short
// random suffix so two pods on the same host still differ.
func resolveReplicaID() string {
	if v := os.Getenv("MHL_SERVE_REPLICA_ID"); v != "" {
		return v
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "replica"
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", host, hex.EncodeToString(b[:]))
}

// runLock is a coarse, per-run execution lease held in the shared KV store.
// Exactly one replica may execute or resume a given runId while it holds the
// lease; the holder renews it on a heartbeat (runLockHeartbeat) and a
// run/status caller tells a live worker from a dead one by whether the lease
// record is still fresh. It is deliberately NOT fenced — a resurrected worker
// that lost its lease could still write a stale checkpoint; that is a later
// increment. The lease only stops two replicas from *starting* work on the
// same run.
//
// A nil *runLock (the store has no "cas" capability, or is the disk store)
// disables coordination entirely — the caller must guard against multiple
// writers itself (e.g. run a single replica).
type runLock struct {
	kv        LockingKVStore
	replicaID string
	now       func() time.Time
}

const (
	runLockTTL       = 30 * time.Second
	runLockHeartbeat = runLockTTL / 3
)

func runLockKey(runID string) string { return kvRunPrefix + runID + "/lock" }

// lockRec is the JSON value stored at runLockKey.
type lockRec struct {
	Holder  string    `json:"holder"`
	RunID   string    `json:"runId"`
	Expires time.Time `json:"expires"`
}

func (r lockRec) fresh(now time.Time) bool { return now.Before(r.Expires) }

// lockState is what peek reports about a run's lease.
type lockState int

const (
	lockAbsent  lockState = iota // no lease record
	lockFresh                    // a lease record whose Expires is in the future
	lockExpired                  // a lease record past its Expires
)

func (l *runLock) fresh() lockRec {
	return lockRec{Holder: l.replicaID, RunID: "", Expires: l.now().Add(runLockTTL)}
}

// acquire tries to take the lease for runID. held is true when this replica now
// owns it — either it was free, or a stale (expired) lease was taken over.
// When held is false, holder names the replica that still holds a fresh lease.
func (l *runLock) acquire(ctx context.Context, runID string) (held bool, holder string, err error) {
	key := runLockKey(runID)
	rec := l.fresh()
	rec.RunID = runID
	ok, err := l.kv.PutIfAbsent(ctx, key, rec)
	if err != nil {
		return false, "", err
	}
	if ok {
		return true, l.replicaID, nil
	}
	// Someone has (or had) it. Take over only if their lease has lapsed.
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return false, "", err
	}
	if !found {
		// Raced with a Delete between PutIfAbsent and Get — try once more.
		ok, err = l.kv.PutIfAbsent(ctx, key, rec)
		return ok, l.replicaID, err
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil {
		// Unparseable lease — overwrite it conditionally on its exact bytes.
		swapped, serr := l.kv.CompareAndSwap(ctx, key, raw, rec)
		return swapped, l.replicaID, serr
	}
	if cur.fresh(l.now()) {
		return false, cur.Holder, nil
	}
	swapped, err := l.kv.CompareAndSwap(ctx, key, raw, rec)
	return swapped, l.replicaID, err
}

// renew extends this replica's lease. stillMine is false (nil error) when the
// lease is gone or now held by someone else — the caller must stop working.
func (l *runLock) renew(ctx context.Context, runID string) (stillMine bool, err error) {
	key := runLockKey(runID)
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil || cur.Holder != l.replicaID {
		return false, nil
	}
	rec := l.fresh()
	rec.RunID = runID
	return l.kv.CompareAndSwap(ctx, key, raw, rec)
}

// release drops the lease. Best-effort: at a run's terminal state the lease is
// still fresh (the heartbeat kept it alive), so an unconditional Delete cannot
// race a takeover.
func (l *runLock) release(ctx context.Context, runID string) error {
	return l.kv.Delete(ctx, runLockKey(runID))
}

// peek reports the current lease state for runID without touching it.
func (l *runLock) peek(ctx context.Context, runID string) (state lockState, holder string) {
	raw, found, err := l.kv.Get(ctx, runLockKey(runID))
	if err != nil || !found {
		return lockAbsent, ""
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil {
		return lockExpired, ""
	}
	if cur.fresh(l.now()) {
		return lockFresh, cur.Holder
	}
	return lockExpired, cur.Holder
}
