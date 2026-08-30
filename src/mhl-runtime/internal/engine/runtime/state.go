// Package runtime provides the pipeline checkpointing & state-recovery
// subsystem (§4 "3. Auth & State Recovery Manager", ADR-4). It persists
// per-step checkpoints under `.mhl/state` and supports resuming a pipeline
// from the step following the last successfully completed/saved step.
//
// Scope: checkpoint persistence and resume continuation only. Parsing of the
// pipeline syntax is consumed from internal/ast and not implemented here, and
// secret redaction of persisted state is out of scope (feature 5).
package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// StateDirName is the checkpoint storage directory, relative to a project root.
const StateDirName = ".mhl/state"

// Checkpoint is the persisted progress of a pipeline. Per DR-1 it records,
// per pipeline, the last completed step, the accumulated variable/session
// state, and a timestamp; entries older than TTL are ignored on resume.
//
// NextStep is the step name Runner.Run already resolved as what comes after
// LastStep — resolved once, at the moment LastStep finished, and persisted
// as-is, never recomputed on resume from declaration order. That
// distinction matters once `goto` exists: a step can redirect execution
// anywhere, so "the step declared right after LastStep" is only ever the
// *default* transition, not necessarily the one that was actually about to
// happen. If resume recomputed it fresh from Pipeline.Steps instead of
// trusting NextStep, a crash between a step finishing and its goto being
// applied would resume down the wrong branch. An empty NextStep means
// LastStep was the pipeline's final step — nothing left to resume.
type Checkpoint struct {
	Pipeline       string         `json:"pipeline"`
	LastStep       string         `json:"last_step"`
	NextStep       string         `json:"next_step"`
	CompletedSteps []string       `json:"completed_steps"`
	Variables      map[string]any `json:"variables"`
	SavedAt        time.Time      `json:"saved_at"`
	TTLSeconds     int64          `json:"ttl_seconds"`
}

// Expired reports whether the checkpoint is older than its TTL as of now. A
// non-positive TTL means the checkpoint never expires.
func (c *Checkpoint) Expired(now time.Time) bool {
	if c.TTLSeconds <= 0 {
		return false
	}
	deadline := c.SavedAt.Add(time.Duration(c.TTLSeconds) * time.Second)
	return now.After(deadline)
}

// StateStore is the checkpoint persistence a Runner performs: load/save/clear
// the in-progress checkpoint for a pipeline, and write a completed run's
// result.json. The built-in implementation is *Store (JSON files under
// .mhl/state); the serve layer supplies an alternative — an extension-backed
// store for a fleet of concurrent runs — via Runner.WithStateStore (Phase 3).
// Session scoping stays a concern of the concrete store: a Runner is always
// handed an already-scoped StateStore.
type StateStore interface {
	Load(pipeline string) (*Checkpoint, bool, error)
	Save(cp *Checkpoint) error
	Clear(pipeline string) error
	WriteResult(pipeline string, vars map[string]any) error
}

var _ StateStore = (*Store)(nil)

// Store persists checkpoints as JSON files under <root>/.mhl/state. When
// Session has scoped it to a per-execution id, dir is <base>/<sessionID> and
// checkpoints for two concurrent runs of the same pipeline never collide;
// base still points at the shared .mhl/state so the .latest pointer can be
// written there.
type Store struct {
	dir       string
	base      string
	sessionID string
	now       func() time.Time
}

// NewStore returns a Store rooted at root; checkpoint files live under
// root/.mhl/state. It is unscoped — call Session to isolate one execution.
func NewStore(root string) *Store {
	dir := filepath.Join(root, StateDirName)
	return &Store{dir: dir, base: dir, now: time.Now}
}

// Session returns a copy of s whose checkpoint files live under
// <base>/<id>, isolating this execution from any other run of the same
// pipeline. An empty id returns s unchanged (the legacy, unscoped layout),
// which is what a --resume that fell back to a pre-session checkpoint, and
// direct-construction unit tests, rely on.
func (s *Store) Session(id string) *Store {
	if id == "" {
		return s
	}
	return &Store{
		dir:       filepath.Join(s.base, id),
		base:      s.base,
		sessionID: id,
		now:       s.now,
	}
}

// WithClock overrides the store's clock; used in tests to simulate TTL expiry.
func (s *Store) WithClock(now func() time.Time) *Store {
	s.now = now
	return s
}

func (s *Store) path(pipeline string) string {
	return filepath.Join(s.dir, pipeline+".json")
}

// Save writes the checkpoint for its pipeline, creating the state directory as
// needed. It stamps SavedAt with the store's clock.
func (s *Store) Save(cp *Checkpoint) error {
	if cp == nil || cp.Pipeline == "" {
		return fmt.Errorf("runtime: cannot save checkpoint without a pipeline name")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("runtime: creating state dir: %w", err)
	}
	cp.SavedAt = s.now()
	redacted := *cp
	redacted.Variables = redactVars(cp.Variables)
	data, err := json.MarshalIndent(&redacted, "", "  ")
	if err != nil {
		return fmt.Errorf("runtime: encoding checkpoint: %w", err)
	}
	tmp := s.path(cp.Pipeline) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("runtime: writing checkpoint: %w", err)
	}
	// Atomic replace so a crash mid-write never leaves a torn file.
	if err := os.Rename(tmp, s.path(cp.Pipeline)); err != nil {
		return fmt.Errorf("runtime: committing checkpoint: %w", err)
	}
	// Record this session as the pipeline's most recent, so a later bare
	// `mhl run --resume` knows which per-session directory to continue.
	if s.sessionID != "" {
		if err := writeLatest(s.base, cp.Pipeline, s.sessionID); err != nil {
			return err
		}
	}
	return nil
}

// Load returns the last valid (non-expired) checkpoint for a pipeline. It
// returns ok=false when no checkpoint exists or when the stored checkpoint is
// past its TTL (DR-1); an expired checkpoint is removed so the pipeline
// restarts cleanly.
func (s *Store) Load(pipeline string) (*Checkpoint, bool, error) {
	data, err := os.ReadFile(s.path(pipeline))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("runtime: reading checkpoint: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, false, fmt.Errorf("runtime: decoding checkpoint: %w", err)
	}
	if cp.Expired(s.now()) {
		// Stale state must not be trusted; drop it.
		_ = s.Clear(pipeline)
		return nil, false, nil
	}
	return &cp, true, nil
}

// Clear removes any checkpoint for a pipeline. It is idempotent. For a
// session-scoped store it also drops the session directory once nothing else
// is left in it, and removes a legacy top-level <base>/<pipeline>.json left
// by a pre-session mhl so a subsequent run doesn't resume from stale state.
func (s *Store) Clear(pipeline string) error {
	err := os.Remove(s.path(pipeline))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("runtime: clearing checkpoint: %w", err)
	}
	if s.sessionID != "" {
		legacy := filepath.Join(s.base, pipeline+".json")
		if rmErr := os.Remove(legacy); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("runtime: clearing legacy checkpoint: %w", rmErr)
		}
		// Best-effort: succeeds only when the session dir is now empty.
		_ = os.Remove(s.dir)
	}
	return nil
}

// redactVars returns a copy of vars with every string value scrubbed through
// auth.Redact. A number, bool, array or object passes through unredacted —
// the same coarse-grained treatment a logged array/object already gets. It
// is shared by checkpoint Save and result.json WriteResult so both persist
// resolved secrets the same way.
func redactVars(vars map[string]any) map[string]any {
	out := make(map[string]any, len(vars))
	for key, value := range vars {
		if s, ok := value.(string); ok {
			out[key] = auth.Redact(s)
		} else {
			out[key] = value
		}
	}
	return out
}

// WriteResult persists a completed run's final variable state as result.json
// in this (session-scoped) store's directory, and records the session as the
// pipeline's most recent — the two things a later run's `context:` element
// reads back. Strings are redacted exactly as a checkpoint's are. A no-op on
// an unscoped store (there is no session directory to write into).
func (s *Store) WriteResult(pipeline string, vars map[string]any) error {
	if s.sessionID == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("runtime: creating session dir: %w", err)
	}
	data, err := json.MarshalIndent(redactVars(vars), "", "  ")
	if err != nil {
		return fmt.Errorf("runtime: encoding result: %w", err)
	}
	tmp := filepath.Join(s.dir, "result.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("runtime: writing result: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, "result.json")); err != nil {
		return fmt.Errorf("runtime: committing result: %w", err)
	}
	return writeLatest(s.base, pipeline, s.sessionID)
}
