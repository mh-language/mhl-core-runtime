package mcpserver

import (
	"context"
	"encoding/json"
	"os"
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

// Key layout. Every run's durable state is under run/<id>/…; sessions under
// session/<id>. Remove(runID) walks run/<id>/.
const (
	kvSessionPrefix = "session/"
	kvRunPrefix     = "run/"
)

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

// extStateStore is the runtime.StateStore a run's Runner writes through
// (injected via execsvc.Request.StateStore). It is scoped to one runID.
type extStateStore struct {
	kv    KVStore
	runID string
}

func newExtStateStore(kv KVStore, runID string) *extStateStore {
	return &extStateStore{kv: kv, runID: runID}
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
	return &cp, true, nil
}

func (s *extStateStore) Save(cp *runtime.Checkpoint) error {
	stamped := *cp
	stamped.SavedAt = time.Now()
	stamped.Variables = runtime.RedactVars(cp.Variables) // never persist resolved secrets
	return s.kv.Put(context.Background(), s.key(cp.Pipeline), &stamped)
}

func (s *extStateStore) Clear(pipeline string) error {
	return s.kv.Delete(context.Background(), s.key(pipeline))
}

func (s *extStateStore) WriteResult(pipeline string, vars map[string]any) error {
	return s.kv.Put(context.Background(), runKey(s.runID, "result"), runtime.RedactVars(vars))
}

var (
	_ SessionStore       = (*extSessionStore)(nil)
	_ CheckpointStore    = (*extCheckpointStore)(nil)
	_ runtime.StateStore = (*extStateStore)(nil)
)
