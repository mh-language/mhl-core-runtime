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
// writers itself (e.g. run a single replica, acknowledged with --single-replica).
type runLock struct {
	kv        LockingKVStore
	replicaID string
	now       func() time.Time
}

// leaseHandle is an immutable reference to one successful acquire. renew and
// release act only on this exact acquisition — a later acquire of the same
// runId (this replica lost the lease and took it back) yields a different
// handle, so a stale holder's late renew or release cannot touch the
// successor's lease. The zero value is not a valid lease.
type leaseHandle struct {
	runID string
	token string
}

func (h leaseHandle) held() bool { return h.token != "" }

const (
	runLockTTL       = 30 * time.Second
	runLockHeartbeat = runLockTTL / 3
	// renewBudget bounds one renew call so a wedged store cannot stall the
	// heartbeat loop past the point a takeover could begin elsewhere.
	renewBudget = 5 * time.Second
)

func runLockKey(runID string) string { return kvRunPrefix + runID + "/lock" }

// lockRec is the JSON value stored at runLockKey.
type lockRec struct {
	Holder  string    `json:"holder"`
	RunID   string    `json:"runId"`
	Token   string    `json:"token,omitempty"`
	Expires time.Time `json:"expires"`
}

func (r lockRec) fresh(now time.Time) bool { return now.Before(r.Expires) }

// lockState is what peek reports about a run's lease.
type lockState int

const (
	lockUnknown lockState = iota // the lease could not be read (store error)
	lockAbsent                   // no lease record
	lockFresh                    // a lease record whose Expires is in the future
	lockExpired                  // a lease record past its Expires
)

func newLeaseToken() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (l *runLock) leaseRec(runID, token string) lockRec {
	return lockRec{Holder: l.replicaID, RunID: runID, Token: token, Expires: l.now().Add(runLockTTL)}
}

// acquire tries to take the lease for runID. On success it returns a leaseHandle
// the caller keeps for the duration of this attempt and passes to renew/release;
// held is true. When held is false and err is nil, holder names the replica that
// still holds a fresh lease. A non-nil err means the store could not be reached:
// the caller must not start work (it cannot rule out another writer).
func (l *runLock) acquire(ctx context.Context, runID string) (lease leaseHandle, held bool, holder string, err error) {
	key := runLockKey(runID)
	token := newLeaseToken()
	rec := l.leaseRec(runID, token)
	mine := leaseHandle{runID: runID, token: token}

	ok, err := l.kv.PutIfAbsent(ctx, key, rec)
	if err != nil {
		return leaseHandle{}, false, "", err
	}
	if ok {
		return mine, true, l.replicaID, nil
	}
	// Someone has (or had) it. Take over only if their lease has lapsed.
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return leaseHandle{}, false, "", err
	}
	if !found {
		// Raced with a release between PutIfAbsent and Get — try once more.
		ok, err = l.kv.PutIfAbsent(ctx, key, rec)
		if err != nil {
			return leaseHandle{}, false, "", err
		}
		if ok {
			return mine, true, l.replicaID, nil
		}
		return leaseHandle{}, false, l.replicaID, nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil {
		// Unparseable lease — overwrite it conditionally on its exact bytes.
		swapped, serr := l.kv.CompareAndSwap(ctx, key, raw, rec)
		if serr != nil {
			return leaseHandle{}, false, "", serr
		}
		if swapped {
			return mine, true, l.replicaID, nil
		}
		return leaseHandle{}, false, l.replicaID, nil
	}
	if cur.fresh(l.now()) {
		return leaseHandle{}, false, cur.Holder, nil
	}
	swapped, err := l.kv.CompareAndSwap(ctx, key, raw, rec)
	if err != nil {
		return leaseHandle{}, false, "", err
	}
	if swapped {
		return mine, true, l.replicaID, nil
	}
	return leaseHandle{}, false, l.replicaID, nil
}

// renew extends the acquisition named by lease. stillMine is false (nil error)
// when the lease is gone or no longer that exact acquisition — the caller must
// stop working. A non-nil error is a transient store failure; the caller decides
// how long to tolerate it before the lease would expire.
func (l *runLock) renew(ctx context.Context, lease leaseHandle) (stillMine bool, err error) {
	if !lease.held() {
		return false, nil
	}
	key := runLockKey(lease.runID)
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil || cur.Holder != l.replicaID || cur.Token != lease.token {
		return false, nil
	}
	rec := l.leaseRec(lease.runID, lease.token) // same acquisition, later expiry
	return l.kv.CompareAndSwap(ctx, key, raw, rec)
}

// release ends the acquisition named by lease. It is conditional on the lease
// still being that exact acquisition: it compare-and-swaps the record to an
// expired tombstone (which the next acquire overwrites, and Remove sweeps) so a
// late release by a replica whose lease already lapsed and was taken over
// cannot wipe the successor's live lease. Not an unconditional Delete.
func (l *runLock) release(ctx context.Context, lease leaseHandle) error {
	if !lease.held() {
		return nil
	}
	key := runLockKey(lease.runID)
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil || cur.Holder != l.replicaID || cur.Token != lease.token {
		return nil // a successor holds it — leave their lease alone
	}
	tombstone := lockRec{RunID: lease.runID, Expires: l.now().Add(-time.Second)}
	_, err = l.kv.CompareAndSwap(ctx, key, raw, tombstone)
	return err
}

// ownsLease reports whether the record at runID is still this exact acquisition
// and fresh. It is the fence check for checkpoint writes: a replica that stalled
// past its lease (and was taken over) gets false here and fails the write. A
// store error is surfaced so the caller can decide (fail closed).
func (l *runLock) ownsLease(ctx context.Context, lease leaseHandle) (bool, error) {
	if !lease.held() {
		return false, nil
	}
	raw, found, err := l.kv.Get(ctx, runLockKey(lease.runID))
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil {
		return false, nil
	}
	return cur.Holder == l.replicaID && cur.Token == lease.token && cur.fresh(l.now()), nil
}

// peek reports the current lease state for runID without touching it.
// lockUnknown (non-nil err) means the store could not be read — the caller must
// not treat that as "no lease".
func (l *runLock) peek(ctx context.Context, runID string) (state lockState, holder string, err error) {
	raw, found, err := l.kv.Get(ctx, runLockKey(runID))
	if err != nil {
		return lockUnknown, "", err
	}
	if !found {
		return lockAbsent, "", nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil {
		return lockExpired, "", nil
	}
	if cur.fresh(l.now()) {
		return lockFresh, cur.Holder, nil
	}
	return lockExpired, cur.Holder, nil
}
