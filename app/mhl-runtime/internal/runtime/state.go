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
)

// StateDirName is the checkpoint storage directory, relative to a project root.
const StateDirName = ".mhl/state"

// Checkpoint is the persisted progress of a pipeline. Per DR-1 it records, per
// pipeline, the last completed step, the accumulated variable/session state,
// and a timestamp; entries older than TTL are ignored on resume.
type Checkpoint struct {
	Pipeline       string            `json:"pipeline"`
	LastStep       string            `json:"last_step"`
	LastStepIndex  int               `json:"last_step_index"`
	CompletedSteps []string          `json:"completed_steps"`
	Variables      map[string]string `json:"variables"`
	SavedAt        time.Time         `json:"saved_at"`
	TTLSeconds     int64             `json:"ttl_seconds"`
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

// Store persists checkpoints as JSON files under <root>/.mhl/state.
type Store struct {
	dir string
	now func() time.Time
}

// NewStore returns a Store rooted at root; checkpoint files live under
// root/.mhl/state.
func NewStore(root string) *Store {
	return &Store{dir: filepath.Join(root, StateDirName), now: time.Now}
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
	data, err := json.MarshalIndent(cp, "", "  ")
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

// Clear removes any checkpoint for a pipeline. It is idempotent.
func (s *Store) Clear(pipeline string) error {
	err := os.Remove(s.path(pipeline))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("runtime: clearing checkpoint: %w", err)
	}
	return nil
}
