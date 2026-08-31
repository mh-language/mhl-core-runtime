package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LoopCheckpoint is the persisted progress of a `loop pipeline` — separate from the
// wrapped pipeline's own per-step Checkpoint, which a LoopRunner resets
// fresh every iteration. This tracks which iteration is next and, once the
// loop has stopped, why: "stop_when" (the condition was satisfied),
// "max_iterations" (the ceiling was hit), or "break" (a step explicitly
// aborted). A LoopCheckpoint with an empty TerminalReason means the loop is
// still in progress — that's the one --resume treats as resumable;
// anything else already ran to a stop and needs a fresh `start`-equivalent
// (a plain, non-resumed run) to go again.
type LoopCheckpoint struct {
	Loop           string    `json:"loop"`
	NextIteration  int       `json:"next_iteration"`
	TerminalReason string    `json:"terminal_reason"`
	SavedAt        time.Time `json:"saved_at"`
	// InstanceID identifies this particular run of the loop, for `mem`
	// (interpreter.MemContext, via Pipeline.InstanceID/RunContext.InstanceID)
	// to namespace a pipeline's persistent variables by — reused across
	// every iteration of one run, and recovered by a --resume of an
	// in-progress one (TerminalReason == ""), but never carried over into a
	// fresh, non-resumed run: see Run's resolution of instanceID below.
	InstanceID string `json:"instance_id,omitempty"`
}

// newInstanceID returns a fresh random hex id for a new (non-resumed) loop
// run. It is an alias for NewSessionID (session.go) — same generator, same
// rationale (no UUID dependency; an opaque filename/JSON-key component) —
// kept under this name because LoopCheckpoint.InstanceID and the `mem`
// backing path already speak of an "instance" id.
func newInstanceID() string {
	return NewSessionID()
}

// LoopStateStore is the per-iteration checkpoint persistence a LoopRunner
// performs. The built-in implementation is *LoopStore (JSON under .mhl/state);
// the serve layer swaps it via LoopRunner.WithLoopStateStore (Phase 3),
// mirroring StateStore for the plain-pipeline path. A completed loop leaves
// its terminal checkpoint in place (no Clear), so the interface has none.
type LoopStateStore interface {
	Load(loop string) (*LoopCheckpoint, bool, error)
	Save(cp *LoopCheckpoint) error
}

var _ LoopStateStore = (*LoopStore)(nil)

// LoopStore persists LoopCheckpoints as JSON files under <root>/.mhl/state,
// alongside (never colliding with) the pipeline Checkpoints Store already
// keeps there — a "loop-" filename prefix keeps the two apart even though a
// loop pipeline's checkpoint and its own per-step Checkpoint share the same
// name.
type LoopStore struct {
	dir       string
	base      string
	sessionID string
	now       func() time.Time
}

// NewLoopStore returns a LoopStore rooted at root. It is unscoped — call
// Session to isolate one execution, the same way Store does.
func NewLoopStore(root string) *LoopStore {
	dir := filepath.Join(root, StateDirName)
	return &LoopStore{dir: dir, base: dir, now: time.Now}
}

// Session returns a copy of s whose loop checkpoint lives under
// <base>/<id>, so it shares one per-execution directory with the wrapped
// pipeline's own Store. An empty id returns s unchanged.
func (s *LoopStore) Session(id string) *LoopStore {
	if id == "" {
		return s
	}
	return &LoopStore{
		dir:       filepath.Join(s.base, id),
		base:      s.base,
		sessionID: id,
		now:       s.now,
	}
}

func (s *LoopStore) path(loop string) string {
	return filepath.Join(s.dir, "loop-"+loop+".json")
}

// Save writes the checkpoint for its loop, creating the state directory as
// needed, atomically (temp file + rename, same pattern as Store.Save — a
// crash mid-write must never leave a torn file).
func (s *LoopStore) Save(cp *LoopCheckpoint) error {
	if cp == nil || cp.Loop == "" {
		return fmt.Errorf("runtime: cannot save a loop checkpoint without a loop name")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("runtime: creating state dir: %w", err)
	}
	cp.SavedAt = s.now()
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("runtime: encoding loop checkpoint: %w", err)
	}
	tmp := s.path(cp.Loop) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("runtime: writing loop checkpoint: %w", err)
	}
	if err := os.Rename(tmp, s.path(cp.Loop)); err != nil {
		return fmt.Errorf("runtime: committing loop checkpoint: %w", err)
	}
	if s.sessionID != "" {
		if err := writeLatest(s.base, cp.Loop, s.sessionID); err != nil {
			return err
		}
	}
	return nil
}

// Load returns the last saved checkpoint for a loop. ok is false when none
// exists yet.
func (s *LoopStore) Load(loop string) (*LoopCheckpoint, bool, error) {
	data, err := os.ReadFile(s.path(loop))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("runtime: reading loop checkpoint: %w", err)
	}
	var cp LoopCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, false, fmt.Errorf("runtime: decoding loop checkpoint: %w", err)
	}
	return &cp, true, nil
}

// LoopResult reports how a loop run ended: how many iterations it completed
// and which of the three stop conditions fired. FinalVars is the last
// completed iteration's accumulated variable state (RunResult.FinalVars),
// nil when no iteration completed — cli.go persists it as the session's
// result.json for a `context:` element, exactly as for a plain pipeline.
type LoopResult struct {
	Iterations     int
	TerminalReason string // "stop_when", "max_iterations", or "break"
	BreakReason    any
	Resumed        bool
	FinalVars      map[string]any
}

// LoopRunner repeats a `loop pipeline`, once per iteration, checkpointing
// after each one — the unit of durable progress a loop needs but a plain
// per-step Checkpoint can't provide on its own: --resume continues at the
// first incomplete iteration, not iteration 0. It is a thin wrapper: each
// iteration reuses Runner.Run completely unmodified, fresh (resume=false)
// every time, since the loop's own checkpoint is what tracks progress
// across iterations — a pipeline's per_step checkpoint (if it even declares
// one) is only ever relevant within the one iteration that's currently
// running.
type LoopRunner struct {
	Runner *Runner
	Store  *LoopStore
	// cp is where loop checkpoints actually go; defaults to Store, swapped by
	// WithLoopStateStore. Mirrors Runner.cp.
	cp LoopStateStore
}

// NewLoopRunner returns a LoopRunner backed by stores rooted at root.
func NewLoopRunner(root string) *LoopRunner {
	s := NewLoopStore(root)
	return &LoopRunner{Runner: NewRunner(root), Store: s, cp: s}
}

// Session scopes both of the LoopRunner's stores to one per-execution
// directory, so a `loop pipeline`'s loop checkpoint and the wrapped
// pipeline's per-step checkpoints share it. An empty id leaves the runner
// unscoped.
func (lr *LoopRunner) Session(id string) *LoopRunner {
	s := lr.Store.Session(id)
	return &LoopRunner{Runner: lr.Runner.Session(id), Store: s, cp: s}
}

// WithLoopStateStore routes the loop's per-iteration checkpoints through cp
// instead of the built-in file LoopStore (Phase 3). cp is expected to be
// scoped to this run already. Returns lr for chaining.
func (lr *LoopRunner) WithLoopStateStore(cp LoopStateStore) *LoopRunner {
	lr.cp = cp
	return lr
}

// loopCheckpoints returns the LoopStateStore this runner persists through.
func (lr *LoopRunner) loopCheckpoints() LoopStateStore {
	if lr.cp != nil {
		return lr.cp
	}
	return lr.Store
}

// Run repeats p, once per iteration via exec, until evalStopWhen reports
// true (checked only after a full iteration completes, never mid-iteration)
// or p.MaxIterations is reached — whichever comes first. A step's `break`
// (surfaced as result.Broke from Runner.Run) always wins over both: it
// stops the loop immediately, without evalStopWhen ever being called for
// that iteration. A genuine error from an iteration is not a soft stop —
// unlike the three terminal reasons above, it propagates straight out,
// aborting the run the same way a plain (non-looped) pipeline's error
// already does. The checkpoint is keyed by p.Name — the loop and the
// pipeline it repeats are now the same declaration, so there's no separate
// loop name to key it by instead.
//
// init is forwarded to each iteration's own lr.Runner.Run(p, init, exec,
// false) call below unchanged — since that call is what resets pipeline-
// level state fresh every time, a loop's pipeline-level `var`s reset on
// every iteration for free, with no loop-specific logic needed here.
//
// p.InstanceID is resolved here — recovered from the loop's own checkpoint
// when resuming an in-progress run (mirroring the iteration resolution just
// below it), or freshly generated otherwise — and then set on the local p
// (a value, so this never mutates the caller's Pipeline) before the very
// first lr.Runner.Run call, so it's already in place for iteration 0, not
// just iterations resumed from a later point. A fresh id on every
// non-resumed run (even a plain re-run with no --resume flag at all) is
// deliberate: two independent runs of the same loop pipeline must never
// share `mem` state just because they share a pipeline name.
func (lr *LoopRunner) Run(runCtx context.Context, p Pipeline, init InitFunc, exec StepFunc, evalStopWhen func(instanceID string) (bool, error), resume bool) (*LoopResult, error) {
	if runCtx == nil {
		runCtx = context.Background()
	}
	iteration := 0
	resumed := false
	var finalVars map[string]any
	instanceID := ""
	if resume {
		cp, ok, err := lr.loopCheckpoints().Load(p.Name)
		if err != nil {
			return nil, err
		}
		if ok && cp.TerminalReason == "" {
			iteration = cp.NextIteration
			instanceID = cp.InstanceID
			resumed = true
		}
	}
	if instanceID == "" {
		instanceID = newInstanceID()
	}
	p.InstanceID = instanceID

	for {
		if err := runCtx.Err(); err != nil {
			return nil, fmt.Errorf("runtime: loop %q cancelled before iteration %d: %w", p.Name, iteration, err)
		}
		if p.MaxIterations > 0 && iteration >= p.MaxIterations {
			if err := lr.loopCheckpoints().Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "max_iterations", InstanceID: instanceID}); err != nil {
				return nil, err
			}
			return &LoopResult{Iterations: iteration, TerminalReason: "max_iterations", Resumed: resumed, FinalVars: finalVars}, nil
		}

		result, err := lr.Runner.Run(runCtx, p, init, exec, false)
		if err != nil {
			return nil, err
		}
		if result.Broke {
			if err := lr.loopCheckpoints().Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "break", InstanceID: instanceID}); err != nil {
				return nil, err
			}
			return &LoopResult{Iterations: iteration + 1, TerminalReason: "break", BreakReason: result.BreakReason, Resumed: resumed, FinalVars: finalVars}, nil
		}
		finalVars = result.FinalVars

		iteration++
		done, err := evalStopWhen(instanceID)
		if err != nil {
			return nil, err
		}
		if done {
			if err := lr.loopCheckpoints().Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "stop_when", InstanceID: instanceID}); err != nil {
				return nil, err
			}
			return &LoopResult{Iterations: iteration, TerminalReason: "stop_when", Resumed: resumed, FinalVars: finalVars}, nil
		}

		if err := lr.loopCheckpoints().Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "", InstanceID: instanceID}); err != nil {
			return nil, err
		}
	}
}
