package runtime

import (
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
}

// LoopStore persists LoopCheckpoints as JSON files under <root>/.mhl/state,
// alongside (never colliding with) the pipeline Checkpoints Store already
// keeps there — a "loop-" filename prefix keeps the two apart even though a
// loop pipeline's checkpoint and its own per-step Checkpoint share the same
// name.
type LoopStore struct {
	dir string
	now func() time.Time
}

// NewLoopStore returns a LoopStore rooted at root.
func NewLoopStore(root string) *LoopStore {
	return &LoopStore{dir: filepath.Join(root, StateDirName), now: time.Now}
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
// and which of the three stop conditions fired.
type LoopResult struct {
	Iterations     int
	TerminalReason string // "stop_when", "max_iterations", or "break"
	BreakReason    any
	Resumed        bool
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
}

// NewLoopRunner returns a LoopRunner backed by stores rooted at root.
func NewLoopRunner(root string) *LoopRunner {
	return &LoopRunner{Runner: NewRunner(root), Store: NewLoopStore(root)}
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
func (lr *LoopRunner) Run(p Pipeline, init InitFunc, exec StepFunc, evalStopWhen func() (bool, error), resume bool) (*LoopResult, error) {
	iteration := 0
	resumed := false
	if resume {
		cp, ok, err := lr.Store.Load(p.Name)
		if err != nil {
			return nil, err
		}
		if ok && cp.TerminalReason == "" {
			iteration = cp.NextIteration
			resumed = true
		}
	}

	for {
		if p.MaxIterations > 0 && iteration >= p.MaxIterations {
			if err := lr.Store.Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "max_iterations"}); err != nil {
				return nil, err
			}
			return &LoopResult{Iterations: iteration, TerminalReason: "max_iterations", Resumed: resumed}, nil
		}

		result, err := lr.Runner.Run(p, init, exec, false)
		if err != nil {
			return nil, err
		}
		if result.Broke {
			if err := lr.Store.Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "break"}); err != nil {
				return nil, err
			}
			return &LoopResult{Iterations: iteration + 1, TerminalReason: "break", BreakReason: result.BreakReason, Resumed: resumed}, nil
		}

		iteration++
		done, err := evalStopWhen()
		if err != nil {
			return nil, err
		}
		if done {
			if err := lr.Store.Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: "stop_when"}); err != nil {
				return nil, err
			}
			return &LoopResult{Iterations: iteration, TerminalReason: "stop_when", Resumed: resumed}, nil
		}

		if err := lr.Store.Save(&LoopCheckpoint{Loop: p.Name, NextIteration: iteration, TerminalReason: ""}); err != nil {
			return nil, err
		}
	}
}
