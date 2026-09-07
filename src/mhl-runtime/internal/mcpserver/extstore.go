package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// KVStore is the narrow key/value contract an external `store`-kind extension
// backs (Phase 3). Keys are opaque strings, values are JSON. The serve
// command builds the concrete extension-backed implementation and passes it in
// via HTTPConfig.Store; the adapters below turn it into the SessionStore /
// CheckpointStore / runtime.StateStore the rest of the server already speaks.
type KVStore interface {
	// Get returns the raw JSON stored at key. found is false (val nil) when
	// the key is absent.
	Get(ctx context.Context, key string) (val []byte, found bool, err error)
	// Put stores value (JSON-marshalled) at key, overwriting.
	Put(ctx context.Context, key string, value any) error
	// Delete removes key. Absent key is not an error.
	Delete(ctx context.Context, key string) error
	// List returns every key with the given prefix.
	List(ctx context.Context, prefix string) ([]string, error)
}

// ErrCASUnsupported is returned by a LockingKVStore's PutIfAbsent /
// CompareAndSwap when the backing extension did not advertise the "cas"
// capability. The caller treats it the same as a nil lock — no cross-replica
// coordination.
var ErrCASUnsupported = errors.New("mcpserver: store does not support compare-and-swap")

// LockingKVStore is a KVStore that also offers the two atomic primitives the
// per-run execution lock (runlock.go) needs. An extension store provides them
// when it advertises the "cas" capability in its initialize handshake;
// without it, CASCapable() reports false and the server disables cross-replica
// run locking (a single writer is then the operator's responsibility).
type LockingKVStore interface {
	KVStore
	// CASCapable reports whether the backing store actually implements the
	// atomic primitives below (the "cas" capability was advertised).
	CASCapable() bool
	// PutIfAbsent stores value at key only if key does not already exist.
	// acquired is false (nil error) when the key is already present.
	PutIfAbsent(ctx context.Context, key string, value any) (acquired bool, err error)
	// CompareAndSwap replaces the value at key with newValue only if its
	// current raw bytes equal expected. swapped is false (nil error) on a
	// value mismatch or a missing key.
	CompareAndSwap(ctx context.Context, key string, expected []byte, newValue any) (swapped bool, err error)
}

// Key layout. Every run's durable state is under run/<id>/…; sessions under
// session/<id>. Remove(runID) walks run/<id>/.
const (
	kvSessionPrefix = "session/"
	kvRunPrefix     = "run/"
)

// statusScanner is an optional KVStore fast path for the durable-intake hot
// loops: fetch every run/<id>/status record in one round-trip, and count the
// pending ones through a store-side index. An extension store advertising the
// "scan" capability implements it (see internal/cli/storext.go); without it the
// extCheckpointStore falls back to a list + a get per run.
type statusScanner interface {
	// ListStatusRecs returns every run/<id>/status record keyed by runID.
	// supported is false when the backing store has no native scan.
	ListStatusRecs(ctx context.Context) (recs map[string]RunStatusRec, supported bool, err error)
	// CountPendingRuns returns how many status records are in the pending
	// state. supported is false when the store cannot count without a scan.
	CountPendingRuns(ctx context.Context) (n int, supported bool, err error)
}

func runKey(runID, suffix string) string { return kvRunPrefix + runID + "/" + suffix }

// --- sessions ---------------------------------------------------------------

// sessionRec is the serialisable form of *session for the KV store.
type sessionRec struct {
	ID          string    `json:"id"`
	Principal   string    `json:"principal,omitempty"`
	Initialized bool      `json:"initialized"`
	Protocol    string    `json:"protocol,omitempty"`
	LastUsed    time.Time `json:"last_used"`
}

type extSessionStore struct{ kv KVStore }

func newExtSessionStore(kv KVStore) *extSessionStore { return &extSessionStore{kv: kv} }

func (s *extSessionStore) Get(id string) (*session, bool) {
	raw, found, err := s.kv.Get(context.Background(), kvSessionPrefix+id)
	if err != nil || !found {
		return nil, false
	}
	var rec sessionRec
	if json.Unmarshal(raw, &rec) != nil {
		return nil, false
	}
	sess := &session{id: rec.ID, principal: rec.Principal, initialized: rec.Initialized, protocol: rec.Protocol}
	// Bump the idle timer, best-effort.
	rec.LastUsed = time.Now()
	_ = s.kv.Put(context.Background(), kvSessionPrefix+id, rec)
	return sess, true
}

func (s *extSessionStore) Put(sess *session) {
	_ = s.kv.Put(context.Background(), kvSessionPrefix+sess.id, sessionRec{
		ID: sess.id, Principal: sess.principal, Initialized: sess.initialized,
		Protocol: sess.protocol, LastUsed: time.Now(),
	})
}

func (s *extSessionStore) Delete(id string) bool {
	_, found, _ := s.kv.Get(context.Background(), kvSessionPrefix+id)
	_ = s.kv.Delete(context.Background(), kvSessionPrefix+id)
	return found
}

func (s *extSessionStore) SweepIdle(olderThan time.Duration) {
	cut := time.Now().Add(-olderThan)
	keys, err := s.kv.List(context.Background(), kvSessionPrefix)
	if err != nil {
		return
	}
	for _, k := range keys {
		raw, found, err := s.kv.Get(context.Background(), k)
		if err != nil || !found {
			continue
		}
		var rec sessionRec
		if json.Unmarshal(raw, &rec) == nil && rec.LastUsed.Before(cut) {
			_ = s.kv.Delete(context.Background(), k)
		}
	}
}

func (s *extSessionStore) Len() int {
	keys, _ := s.kv.List(context.Background(), kvSessionPrefix)
	return len(keys)
}

// --- checkpoints (reconstruct + owner) ------------------------------------

type extCheckpointStore struct {
	kv      KVStore
	scratch string // a per-process temp dir execsvc still needs for its own scratch
}

func newExtCheckpointStore(kv KVStore) (*extCheckpointStore, error) {
	dir, err := os.MkdirTemp("", "mhl-serve-extstore-")
	if err != nil {
		return nil, err
	}
	return &extCheckpointStore{kv: kv, scratch: dir}, nil
}

func (c *extCheckpointStore) BaseDir() string { return c.scratch }

func (c *extCheckpointStore) Exists(runID string) bool {
	keys, err := c.kv.List(context.Background(), runKey(runID, "checkpoint/"))
	return err == nil && len(keys) > 0
}

func (c *extCheckpointStore) Load(runID string) (*runtime.Checkpoint, bool) {
	keys, err := c.kv.List(context.Background(), runKey(runID, "checkpoint/"))
	if err != nil || len(keys) == 0 {
		return nil, false
	}
	raw, found, err := c.kv.Get(context.Background(), keys[0])
	if err != nil || !found {
		return nil, false
	}
	var cp runtime.Checkpoint
	if json.Unmarshal(raw, &cp) != nil {
		return nil, false
	}
	return &cp, true
}

func (c *extCheckpointStore) WriteOwner(runID string, o Owner) error {
	if o == "" {
		return nil
	}
	return c.kv.Put(context.Background(), runKey(runID, "owner"), string(o))
}

func (c *extCheckpointStore) ReadOwner(runID string) (Owner, bool) {
	raw, found, err := c.kv.Get(context.Background(), runKey(runID, "owner"))
	if err != nil || !found {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return "", false
	}
	return Owner(s), true
}

func (c *extCheckpointStore) Remove(runID string) error {
	keys, err := c.kv.List(context.Background(), kvRunPrefix+runID+"/")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if delErr := c.kv.Delete(context.Background(), k); delErr != nil && err == nil {
			err = delErr
		}
	}
	return err
}

func (c *extCheckpointStore) Close() error { return os.RemoveAll(c.scratch) }

// An extension store is by definition shared across replicas.
func (c *extCheckpointStore) Shared() bool { return true }

func (c *extCheckpointStore) WriteStatus(runID string, rec RunStatusRec) error {
	return c.kv.Put(context.Background(), runKey(runID, "status"), rec)
}

func (c *extCheckpointStore) ReadStatus(runID string) (RunStatusRec, bool) {
	raw, found, err := c.kv.Get(context.Background(), runKey(runID, "status"))
	if err != nil || !found {
		return RunStatusRec{}, false
	}
	var rec RunStatusRec
	if json.Unmarshal(raw, &rec) != nil {
		return RunStatusRec{}, false
	}
	return rec, true
}

func (c *extCheckpointStore) ListStatuses() (map[string]RunStatusRec, error) {
	// Fast path: an extension with the "scan" capability returns every status
	// record in one query, sparing the reconcile / sweep loops a get per run.
	if sc, ok := c.kv.(statusScanner); ok {
		if recs, supported, err := sc.ListStatusRecs(context.Background()); supported {
			return recs, err
		}
	}
	keys, err := c.kv.List(context.Background(), kvRunPrefix)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RunStatusRec)
	for _, k := range keys {
		rest := strings.TrimPrefix(k, kvRunPrefix)
		id, ok := strings.CutSuffix(rest, "/status")
		if !ok || id == "" || strings.Contains(id, "/") {
			continue
		}
		if rec, found := c.ReadStatus(id); found {
			out[id] = rec
		}
	}
	return out, nil
}

// CountPending uses the extension's "scan" fast path (a COUNT served by the
// pending partial index) when available; ok=false otherwise, so the caller
// counts from ListStatuses.
func (c *extCheckpointStore) CountPending() (int, bool, error) {
	sc, ok := c.kv.(statusScanner)
	if !ok {
		return 0, false, nil
	}
	return sc.CountPendingRuns(context.Background())
}

func (c *extCheckpointStore) RequestCancel(runID string) error {
	return c.kv.Put(context.Background(), runKey(runID, "cancel"), true)
}

func (c *extCheckpointStore) CancelRequested(runID string) bool {
	raw, found, err := c.kv.Get(context.Background(), runKey(runID, "cancel"))
	if err != nil || !found {
		return false
	}
	var v bool
	return json.Unmarshal(raw, &v) == nil && v
}

// --- per-step checkpoints / result (runtime.StateStore) -----------------

// FencedWriter is an optional KVStore capability: a write conditioned, in one
// atomic store operation, on the run's lease record at lockKey still naming
// this holder and token and being unexpired. It closes the check-then-write
// TOCTOU window — a replica whose lease was taken over has its checkpoint
// writes rejected by the store itself, not merely by a prior local check.
type FencedWriter interface {
	// FenceCapable reports whether the backing store actually implements the
	// atomic fenced writes below (the "fence" capability was advertised).
	FenceCapable() bool
	PutFenced(ctx context.Context, key string, value any, lockKey, holder, token string) (written bool, err error)
	DeleteFenced(ctx context.Context, key, lockKey, holder, token string) (deleted bool, err error)
}

// stateFence carries what a fenced write needs. When fw is non-nil the write is
// atomic store-side; otherwise check() runs first (check-then-write, a narrow
// TOCTOU window). nil *stateFence ⇒ writes are unfenced (no run lock).
type stateFence struct {
	fw      FencedWriter
	check   func() error
	lockKey string
	holder  string
	token   string
}

// extStateStore is the runtime.StateStore a run's Runner writes through
// (injected via execsvc.Request.StateStore). It is scoped to one runID. When
// fence is set, every mutating write (Save / Clear) is gated on this replica
// still holding the run's execution lease — a replica taken over after a stall
// fails its writes (ErrLeaseLost) instead of corrupting the successor's state.
type extStateStore struct {
	kv    KVStore
	runID string
	fence *stateFence
}

func newExtStateStore(kv KVStore, runID string, fence *stateFence) *extStateStore {
	return &extStateStore{kv: kv, runID: runID, fence: fence}
}

// ErrLeaseLost is returned by a fenced extStateStore write when this replica no
// longer holds the run's execution lease.
var ErrLeaseLost = errors.New("mcpserver: run execution lease lost — refusing checkpoint write")

// fencedPut writes value at key subject to the lease fence.
func (s *extStateStore) fencedPut(key string, value any) error {
	if s.fence == nil {
		return s.kv.Put(context.Background(), key, value)
	}
	if s.fence.fw != nil {
		ok, err := s.fence.fw.PutFenced(context.Background(), key, value, s.fence.lockKey, s.fence.holder, s.fence.token)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLeaseLost
		}
		return nil
	}
	if err := s.fence.check(); err != nil {
		return err
	}
	return s.kv.Put(context.Background(), key, value)
}

// fencedDelete removes key subject to the lease fence.
func (s *extStateStore) fencedDelete(key string) error {
	if s.fence == nil {
		return s.kv.Delete(context.Background(), key)
	}
	if s.fence.fw != nil {
		ok, err := s.fence.fw.DeleteFenced(context.Background(), key, s.fence.lockKey, s.fence.holder, s.fence.token)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLeaseLost
		}
		return nil
	}
	if err := s.fence.check(); err != nil {
		return err
	}
	return s.kv.Delete(context.Background(), key)
}

func (s *extStateStore) key(pipeline string) string {
	return runKey(s.runID, "checkpoint/"+pipeline)
}

func (s *extStateStore) Load(pipeline string) (*runtime.Checkpoint, bool, error) {
	raw, found, err := s.kv.Get(context.Background(), s.key(pipeline))
	if err != nil || !found {
		return nil, false, err
	}
	var cp runtime.Checkpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, false, err
	}
	if cp.Expired(time.Now()) {
		_ = s.Clear(pipeline)
		return nil, false, nil
	}
	if cp.Variables != nil {
		vars, err := runtime.RehydrateVars(cp.Variables)
		if err != nil {
			return nil, false, err
		}
		cp.Variables = vars
	}
	return &cp, true, nil
}

func (s *extStateStore) Save(cp *runtime.Checkpoint) error {
	stamped := *cp
	stamped.SavedAt = time.Now()
	// Never persist resolved secrets; a secret with a known credential
	// reference is stored as a re-resolvable placeholder (rehydrated in Load),
	// everything else is masked.
	stamped.Variables = runtime.RedactVarsForCheckpoint(cp.Variables)
	return s.fencedPut(s.key(cp.Pipeline), &stamped)
}

func (s *extStateStore) Clear(pipeline string) error {
	return s.fencedDelete(s.key(pipeline))
}

func (s *extStateStore) WriteResult(pipeline string, vars map[string]any) error {
	return s.kv.Put(context.Background(), runKey(s.runID, "result"), runtime.RedactVars(vars))
}

var (
	_ SessionStore       = (*extSessionStore)(nil)
	_ CheckpointStore    = (*extCheckpointStore)(nil)
	_ runtime.StateStore = (*extStateStore)(nil)
)
