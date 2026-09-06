// Package runtime provides the pipeline checkpointing & state-recovery
// subsystem (§4 "3. Auth & State Recovery Manager", ADR-4). It persists
// per-step checkpoints under `.mhl/state` and supports resuming a pipeline
// from the step following the last successfully completed/saved step.
//
// Scope: checkpoint persistence and resume continuation only. Parsing of the
// pipeline syntax is consumed from internal/ast and not implemented here.
// Resolved secrets in persisted state are masked recursively on the way out
// (RedactVars / redactValue), shared with every other run-variable output
// boundary.
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

	// DefinitionDigest binds this checkpoint to the exact program structure it
	// was written for (DefinitionDigest of the resolved AST). A --resume whose
	// current definition digests differently is refused — resuming would run
	// the persisted variable state through changed control flow. Empty on a
	// checkpoint written before this field existed (resume is then allowed,
	// for backward compatibility).
	DefinitionDigest string `json:"definition_digest,omitempty"`
	// RuntimeVersion is the build that wrote the checkpoint — informational,
	// surfaced on a resume; never a resume gate on its own.
	RuntimeVersion string `json:"runtime_version,omitempty"`
	// StateSchema is the Checkpoint on-disk format version (StateSchemaVersion).
	// A checkpoint from a newer schema than this build knows is refused. 0 (the
	// zero value, from a pre-versioning checkpoint) is treated as compatible.
	StateSchema int `json:"state_schema,omitempty"`
}

// ErrCheckpointDefinitionMismatch is returned by a resume whose current
// pipeline definition no longer matches the one the checkpoint was written
// for. It is deliberately not recoverable automatically: the operator either
// restores the original .mh or re-runs without --resume.
var ErrCheckpointDefinitionMismatch = fmt.Errorf("runtime: checkpoint was written for a different pipeline definition")

// ErrCheckpointSchemaTooNew is returned when a checkpoint's StateSchema is
// higher than this build's StateSchemaVersion — a newer mhl wrote it.
var ErrCheckpointSchemaTooNew = fmt.Errorf("runtime: checkpoint state schema is newer than this runtime supports")

// CompatibleWith reports whether this checkpoint may be resumed by a build
// whose current definition digest is wantDigest. An empty stored digest (a
// pre-versioning checkpoint) or an empty wantDigest (the caller opted out of
// the check) passes. A newer state schema always fails.
func (c *Checkpoint) CompatibleWith(wantDigest string) error {
	if c.StateSchema > StateSchemaVersion {
		return fmt.Errorf("%w (%d > %d)", ErrCheckpointSchemaTooNew, c.StateSchema, StateSchemaVersion)
	}
	if wantDigest == "" || c.DefinitionDigest == "" {
		return nil
	}
	if c.DefinitionDigest != wantDigest {
		return fmt.Errorf("%w (checkpoint %s..., current %s...); restore the original .mh or re-run without --resume",
			ErrCheckpointDefinitionMismatch, short(c.DefinitionDigest), short(wantDigest))
	}
	return nil
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
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
	// Checkpoint state is resumed, so a secret with a known credential
	// reference is stored as a re-resolvable placeholder rather than a dead
	// mask (RehydrateVars restores it on Load).
	redacted.Variables = RedactVarsForCheckpoint(cp.Variables)
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
	if cp.Variables != nil {
		vars, err := RehydrateVars(cp.Variables)
		if err != nil {
			return nil, false, err
		}
		cp.Variables = vars
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

// RedactVars returns a deep copy of vars with every string value scrubbed
// through auth.Redact, recursing into nested objects and arrays so a secret
// stored one level down (`{"creds": {"token": "…"}}`, `["…"]`) is masked too.
// Numbers and bools pass through untouched. It is shared by checkpoint Save
// and result.json WriteResult so both persist resolved secrets the same way;
// an alternative StateStore implementation (an extension-backed store) must
// call it before persisting too, and any other output boundary that returns
// run variables (protocol replies, run logs) should route through
// redactValue as well.
func RedactVars(vars map[string]any) map[string]any {
	out := make(map[string]any, len(vars))
	for key, value := range vars {
		out[key] = redactValue(value)
	}
	return out
}

// redactValue applies auth.Redact to every string reachable from v, copying
// any map or slice it descends into so the caller's structure is left
// unmodified. Map shapes other than map[string]any (map[any]any from a
// decoded literal) and []any are both handled; unknown types pass through.
func redactValue(v any) any {
	switch t := v.(type) {
	case string:
		return auth.Redact(t)
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = redactValue(val)
		}
		return m
	case map[any]any:
		m := make(map[any]any, len(t))
		for k, val := range t {
			m[k] = redactValue(val)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = redactValue(val)
		}
		return s
	case []string:
		s := make([]string, len(t))
		for i, val := range t {
			s[i] = auth.Redact(val)
		}
		return s
	default:
		return v
	}
}

// RedactValue is the exported entry point for redacting an arbitrary value
// (a single variable, a protocol result payload) with the same recursive
// policy RedactVars applies to a checkpoint's variable map.
func RedactValue(v any) any { return redactValue(v) }

// secretRefKey is the sole field of the placeholder object a checkpoint
// stores in place of a resolved secret whose credential reference is known.
// RehydrateVars re-resolves it on load, so a fresh-process --resume recovers
// the live value instead of a dead mask.
const secretRefKey = "__mhl_secret_ref__"

// ErrNonSerializableVar is wrapped by the error CheckpointableVars returns
// for a pipeline variable holding a value a checkpoint cannot faithfully
// round-trip through JSON (a closure, a function, a channel, an opaque
// handle). Persisting it would silently drop the value to `{}` and a
// --resume would then fail far from the cause, so the run fails here instead.
var ErrNonSerializableVar = fmt.Errorf("runtime: pipeline variable is not checkpointable")

// CheckpointableVars reports the first pipeline variable whose value cannot
// be checkpointed. Checkpoint state is JSON: only nil, strings, numbers,
// bools, and arrays/objects of those survive a Save/Load round-trip
// (resolved secrets are additionally stored as re-resolvable references, see
// RedactVarsForCheckpoint). A value of any other kind — most often a closure
// assigned to a pipeline-scoped `var` — is rejected up front.
func CheckpointableVars(vars map[string]any) error {
	for key, value := range vars {
		if t, ok := firstUnserializable(value); ok {
			return fmt.Errorf("%w: %q holds a %s — assign only strings, numbers, bools, arrays and objects to a pipeline-scoped var (a closure or handle cannot be resumed)", ErrNonSerializableVar, key, t)
		}
	}
	return nil
}

// firstUnserializable returns the type description of the first value
// reachable from v that JSON cannot represent, or ok=false when the whole
// structure is checkpoint-safe.
func firstUnserializable(v any) (string, bool) {
	switch t := v.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		json.Number:
		return "", false
	case []any:
		for _, e := range t {
			if d, bad := firstUnserializable(e); bad {
				return d, true
			}
		}
		return "", false
	case []string:
		return "", false
	case map[string]any:
		for _, e := range t {
			if d, bad := firstUnserializable(e); bad {
				return d, true
			}
		}
		return "", false
	case map[any]any:
		for _, e := range t {
			if d, bad := firstUnserializable(e); bad {
				return d, true
			}
		}
		return "", false
	default:
		return fmt.Sprintf("%T", v), true
	}
}

// RedactVarsForCheckpoint is RedactVars for state that will be resumed: a
// string that is exactly a resolved secret with a known reference
// (auth.RefFor) is replaced by a {secretRefKey: ref} placeholder rather than
// the "[REDACTED]" mask, so RehydrateVars can restore it. Every other string
// (including a secret only embedded as a substring) is masked exactly as
// RedactVars does.
func RedactVarsForCheckpoint(vars map[string]any) map[string]any {
	out := make(map[string]any, len(vars))
	for key, value := range vars {
		out[key] = redactValueForCheckpoint(value)
	}
	return out
}

func redactValueForCheckpoint(v any) any {
	switch t := v.(type) {
	case string:
		if ref, ok := auth.RefFor(t); ok {
			return map[string]any{secretRefKey: ref}
		}
		return auth.Redact(t)
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = redactValueForCheckpoint(val)
		}
		return m
	case map[any]any:
		m := make(map[any]any, len(t))
		for k, val := range t {
			m[k] = redactValueForCheckpoint(val)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = redactValueForCheckpoint(val)
		}
		return s
	case []string:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = redactValueForCheckpoint(val)
		}
		return s
	default:
		return v
	}
}

// RehydrateVars walks a checkpoint's decoded variables and replaces every
// {secretRefKey: ref} placeholder written by RedactVarsForCheckpoint with the
// value re-resolved from that reference. A reference that no longer resolves
// (the environment variable is gone) is a hard error: a resumed run must not
// silently proceed with a missing credential.
func RehydrateVars(vars map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(vars))
	for key, value := range vars {
		rv, err := rehydrateValue(value)
		if err != nil {
			return nil, fmt.Errorf("runtime: restoring checkpoint variable %q: %w", key, err)
		}
		out[key] = rv
	}
	return out, nil
}

func rehydrateValue(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := placeholderRef(t); ok {
			return auth.Resolve(ref)
		}
		m := make(map[string]any, len(t))
		for k, val := range t {
			rv, err := rehydrateValue(val)
			if err != nil {
				return nil, err
			}
			m[k] = rv
		}
		return m, nil
	case map[any]any:
		m := make(map[any]any, len(t))
		for k, val := range t {
			rv, err := rehydrateValue(val)
			if err != nil {
				return nil, err
			}
			m[k] = rv
		}
		return m, nil
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			rv, err := rehydrateValue(val)
			if err != nil {
				return nil, err
			}
			s[i] = rv
		}
		return s, nil
	default:
		return v, nil
	}
}

// placeholderRef reports whether m is exactly a {secretRefKey: "<ref>"}
// placeholder and returns the reference string.
func placeholderRef(m map[string]any) (string, bool) {
	if len(m) != 1 {
		return "", false
	}
	ref, ok := m[secretRefKey].(string)
	return ref, ok && ref != ""
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
	data, err := json.MarshalIndent(RedactVars(vars), "", "  ")
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
