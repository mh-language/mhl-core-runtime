package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
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

	// tokens records, per runID, the nonce of the lease acquisition this
	// replica currently holds. renew and release act only on that exact
	// acquisition: the replica ID alone cannot tell one acquisition from a
	// later one (this replica lost the lease, another took over, this replica
	// re-acquired), so a stale holder must not renew or delete a successor's
	// lease.
	mu     sync.Mutex
	tokens map[string]string
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

func (l *runLock) setToken(runID, token string) {
	l.mu.Lock()
	if l.tokens == nil {
		l.tokens = map[string]string{}
	}
	l.tokens[runID] = token
	l.mu.Unlock()
}

func (l *runLock) getToken(runID string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tokens[runID]
}

func (l *runLock) clearToken(runID string) {
	l.mu.Lock()
	delete(l.tokens, runID)
	l.mu.Unlock()
}

func (l *runLock) leaseRec(runID, token string) lockRec {
	return lockRec{Holder: l.replicaID, RunID: runID, Token: token, Expires: l.now().Add(runLockTTL)}
}

// acquire tries to take the lease for runID. held is true when this replica now
// owns it — either it was free, or a stale (expired) lease was taken over.
// When held is false and err is nil, holder names the replica that still holds
// a fresh lease. A non-nil err means the store could not be reached: the caller
// must not start work (it cannot rule out another writer).
func (l *runLock) acquire(ctx context.Context, runID string) (held bool, holder string, err error) {
	key := runLockKey(runID)
	token := newLeaseToken()
	rec := l.leaseRec(runID, token)

	ok, err := l.kv.PutIfAbsent(ctx, key, rec)
	if err != nil {
		return false, "", err
	}
	if ok {
		l.setToken(runID, token)
		return true, l.replicaID, nil
	}
	// Someone has (or had) it. Take over only if their lease has lapsed.
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return false, "", err
	}
	if !found {
		// Raced with a release between PutIfAbsent and Get — try once more.
		ok, err = l.kv.PutIfAbsent(ctx, key, rec)
		if err != nil {
			return false, "", err
		}
		if ok {
			l.setToken(runID, token)
		}
		return ok, l.replicaID, nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil {
		// Unparseable lease — overwrite it conditionally on its exact bytes.
		swapped, serr := l.kv.CompareAndSwap(ctx, key, raw, rec)
		if serr != nil {
			return false, "", serr
		}
		if swapped {
			l.setToken(runID, token)
		}
		return swapped, l.replicaID, nil
	}
	if cur.fresh(l.now()) {
		return false, cur.Holder, nil
	}
	swapped, err := l.kv.CompareAndSwap(ctx, key, raw, rec)
	if err != nil {
		return false, "", err
	}
	if swapped {
		l.setToken(runID, token)
	}
	return swapped, l.replicaID, nil
}

// renew extends this replica's current acquisition of the lease. stillMine is
// false (nil error) when the lease is gone or no longer this exact acquisition
// — the caller must stop working. A non-nil error is a transient store failure;
// the caller decides how long to tolerate it before the lease would expire.
func (l *runLock) renew(ctx context.Context, runID string) (stillMine bool, err error) {
	token := l.getToken(runID)
	if token == "" {
		return false, nil
	}
	key := runLockKey(runID)
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil || cur.Holder != l.replicaID || cur.Token != token {
		return false, nil
	}
	rec := l.leaseRec(runID, token) // same acquisition, later expiry
	return l.kv.CompareAndSwap(ctx, key, raw, rec)
}

// release ends this replica's acquisition. It is conditional on the lease still
// being that exact acquisition: it compare-and-swaps the record to an expired
// tombstone (which the next acquire overwrites, and Remove sweeps) so a late
// release by a replica whose lease already lapsed and was taken over cannot
// wipe the successor's live lease. Not an unconditional Delete.
func (l *runLock) release(ctx context.Context, runID string) error {
	token := l.getToken(runID)
	defer l.clearToken(runID)
	if token == "" {
		return nil
	}
	key := runLockKey(runID)
	raw, found, err := l.kv.Get(ctx, key)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	var cur lockRec
	if json.Unmarshal(raw, &cur) != nil || cur.Holder != l.replicaID || cur.Token != token {
		return nil // a successor holds it — leave their lease alone
	}
	tombstone := lockRec{RunID: runID, Expires: l.now().Add(-time.Second)}
	_, err = l.kv.CompareAndSwap(ctx, key, raw, tombstone)
	return err
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
