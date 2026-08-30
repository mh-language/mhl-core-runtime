package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// This file holds the state seams the HTTP transport is built on. Each
// interface has exactly one implementation today — an in-memory map or the
// on-disk .mhl/state tree, byte-for-byte the behaviour the handler had inline
// before. Phase 3 adds a second implementation of each (an external
// `store`-kind extension) so a fleet of replicas can share run and session
// state; nothing at the call sites changes when it does.

// Owner identifies the principal that owns an async run. Today it is the
// SHA-256 of the run's originating Mcp-Session-Id (see ownerFromSession),
// hashed so a leaked run view carries no usable session credential; Phase 2
// fills it from a verified caller principal instead. A named type so the
// run/status / run/resume / run/cancel / run/list guards read as an
// authorization check, not a string compare.
type Owner string

// ownerFromSession derives the Owner of a run started by the session with the
// given id. An empty id (a stateless caller) yields the shared anonymous
// Owner "" — stateless mode has no per-caller run isolation.
func ownerFromSession(sessionID string) Owner {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return Owner(hex.EncodeToString(sum[:]))
}

// --- sessions -------------------------------------------------------------

// SessionStore holds the MCP protocol sessions minted by `initialize` and
// keyed by the Mcp-Session-Id header. The default (memSessionStore) is a
// process-local map; an idle session is swept after sessionTTL.
type SessionStore interface {
	// Get returns the session for id and bumps its idle timer, or ok=false
	// when it is unknown or already swept.
	Get(id string) (s *session, ok bool)
	// Put stores s under s.id, (re)starting its idle timer.
	Put(s *session)
	// Delete drops the session for id, reporting whether it was present.
	Delete(id string) bool
	// SweepIdle drops every session untouched for longer than olderThan.
	SweepIdle(olderThan time.Duration)
}

// httpSession is one stored protocol session plus its last-touched time.
type httpSession struct {
	sess     *session
	lastUsed time.Time
}

type memSessionStore struct {
	mu sync.Mutex
	m  map[string]*httpSession
}

func newMemSessionStore() *memSessionStore {
	return &memSessionStore{m: map[string]*httpSession{}}
}

func (s *memSessionStore) Get(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hs := s.m[id]
	if hs == nil {
		return nil, false
	}
	hs.lastUsed = time.Now()
	return hs.sess, true
}

func (s *memSessionStore) Put(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[sess.id] = &httpSession{sess: sess, lastUsed: time.Now()}
}

func (s *memSessionStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[id]
	delete(s.m, id)
	return ok
}

func (s *memSessionStore) SweepIdle(olderThan time.Duration) {
	cut := time.Now().Add(-olderThan)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, hs := range s.m {
		if hs.lastUsed.Before(cut) {
			delete(s.m, id)
		}
	}
}

// --- run registry -------------------------------------------------------

// RunRegistry tracks the async runs started by run/start for the life of the
// process. The default (memRunRegistry) is a process-local map; a resumable
// run is kept until swept on `initialize` (see sweepRuns), a completed one
// until sessionTTL passes.
type RunRegistry interface {
	Get(id string) (r *asyncRun, ok bool)
	Put(r *asyncRun)
	Delete(id string)
	// List returns an unordered snapshot of the tracked runs; mutating the
	// registry while ranging over it is safe.
	List() []*asyncRun
}

type memRunRegistry struct {
	mu sync.Mutex
	m  map[string]*asyncRun
}

func newMemRunRegistry() *memRunRegistry {
	return &memRunRegistry{m: map[string]*asyncRun{}}
}

func (r *memRunRegistry) Get(id string) (*asyncRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rn, ok := r.m[id]
	return rn, ok
}

func (r *memRunRegistry) Put(rn *asyncRun) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[rn.id] = rn
}

func (r *memRunRegistry) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

func (r *memRunRegistry) List() []*asyncRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*asyncRun, 0, len(r.m))
	for _, rn := range r.m {
		out = append(out, rn)
	}
	return out
}

// --- checkpoint store -------------------------------------------------

// CheckpointStore is the view of the on-disk .mhl/state tree that runs.go
// needs: where a run's checkpoint directory lives, whether it holds a
// resumable checkpoint, that checkpoint's parsed contents (for
// reconstructRun after a restart), and removal of a finished run's state.
// BaseDir is what execsvc.Request.BaseDir is set to for every run.
type CheckpointStore interface {
	BaseDir() string
	StateDir(runID string) string
	// Exists reports whether runID has any checkpoint file on disk (a
	// <pipeline>.json that is not result.json), parseable or not.
	Exists(runID string) bool
	// Load returns runID's first parseable checkpoint, or ok=false when the
	// directory is missing or holds none.
	Load(runID string) (cp *runtime.Checkpoint, ok bool)
	// Remove deletes runID's entire state directory. Idempotent.
	Remove(runID string) error
	// Close releases the store; the disk implementation removes a
	// process-owned temp directory (a caller-supplied --state-dir is kept).
	Close() error
}

// diskCheckpointStore is the built-in CheckpointStore: run state lives under
// <runsDir>/.mhl/state/<runID>/. runsDir is a caller-supplied --state-dir
// (durable, kept on Close) or a per-process temp dir (owned, removed on
// Close).
type diskCheckpointStore struct {
	runsDir string
	owns    bool
}

// newDiskCheckpointStore resolves stateDir: "" mints an owned temp dir,
// otherwise the given path is created if needed and left in place on Close.
func newDiskCheckpointStore(stateDir string) (*diskCheckpointStore, error) {
	if stateDir == "" {
		dir, err := os.MkdirTemp("", "mhl-serve-mcp-")
		if err != nil {
			return nil, err
		}
		return &diskCheckpointStore{runsDir: dir, owns: true}, nil
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	return &diskCheckpointStore{runsDir: stateDir, owns: false}, nil
}

func (d *diskCheckpointStore) BaseDir() string { return d.runsDir }

func (d *diskCheckpointStore) StateDir(runID string) string {
	return filepath.Join(d.runsDir, runtime.StateDirName, runID)
}

func (d *diskCheckpointStore) Exists(runID string) bool {
	entries, err := os.ReadDir(d.StateDir(runID))
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".json") && n != "result.json" {
			return true
		}
	}
	return false
}

func (d *diskCheckpointStore) Load(runID string) (*runtime.Checkpoint, bool) {
	stateDir := d.StateDir(runID)
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || name == "result.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stateDir, name))
		if err != nil {
			continue
		}
		var cp runtime.Checkpoint
		if json.Unmarshal(data, &cp) != nil {
			continue
		}
		return &cp, true
	}
	return nil, false
}

func (d *diskCheckpointStore) Remove(runID string) error {
	return os.RemoveAll(d.StateDir(runID))
}

func (d *diskCheckpointStore) Close() error {
	if d.owns {
		return os.RemoveAll(d.runsDir)
	}
	return nil
}
