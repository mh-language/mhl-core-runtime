package runtime

import (
	"errors"
	"fmt"
)

// RunContext carries the accumulated variable/session state across steps. It is
// persisted into and restored from checkpoints. Vars is deliberately
// map[string]any, not map[string]string: cli.go's --input flags only ever
// put strings in here, but InitFunc (below) also stores a pipeline's
// top-level `var` values here, which — like any interpreter.Env — can hold
// numbers, bools, arrays and objects, not just strings.
type RunContext struct {
	Vars map[string]any
	// InstanceID identifies which run of this pipeline is executing, for
	// cli.go's `mem` support (interpreter.MemContext) to namespace a
	// pipeline's persistent variables by. Set from Pipeline.InstanceID
	// (below) when Run allocates ctx — "default" for a plain pipeline
	// (Pipeline.InstanceID is only ever populated by LoopRunner), or a
	// loop's per-run/per-resume id for a `loop pipeline`, so a `mem` var
	// stays isolated between two independent (non-resumed) runs of the same
	// loop but shared across all iterations of one run — see LoopRunner.Run.
	InstanceID string
}

// StepFunc executes a single step. Returning a plain error aborts the
// pipeline; the last successfully completed step remains checkpointed for a
// later --resume. Returning a *BreakSignal or *GotoSignal instead (see
// signal.go) is not a failure — cli.go's exec closure produces these from
// interpreter.IsBreak/IsGoto — and Run reacts to each differently, per their
// doc comments.
type StepFunc func(step string, ctx *RunContext) error

// InitFunc runs once at the very start of a fresh (non-resumed) Run() —
// before its first step — to seed ctx.Vars. cli.go's implementation
// evaluates the pipeline's top-level `var` declarations
// (interpreter.EvalPipelineVars) into it; a pipeline with none of those can
// pass a nil InitFunc, which Run treats as a no-op. On a resumed run,
// init is skipped entirely and ctx.Vars is restored from the checkpoint
// instead (the same "don't recompute what may have moved on" reasoning
// NextStep's doc comment already gives for the step side of resume).
type InitFunc func(ctx *RunContext) error

// RunResult reports which steps actually executed and which were skipped by
// a resume, plus how the run ended when that wasn't by simply completing:
// Broke is true when a step's `break` stopped the run outright, with
// BreakReason carrying whatever value (if any) it evaluated.
type RunResult struct {
	Executed    []string
	Skipped     []string
	Resumed     bool
	Broke       bool
	BreakReason any
}

// maxStepVisits caps how many times Run may (re-)enter the same step name,
// so a `goto` cycle with no `break` to escape it (e.g. `goto A` inside step
// A itself) fails with a clear error instead of hanging `mhl run` forever —
// the same safety cap internal/engine/interpreter's maxLoopIterations
// already applies to a runaway `while`.
const maxStepVisits = 10_000

// Runner executes a pipeline's steps in order, checkpointing per step when
// the pipeline's checkpoint config is enabled with the per_step strategy.
type Runner struct {
	Store *Store
}

// NewRunner returns a Runner backed by a Store rooted at root.
func NewRunner(root string) *Runner {
	return &Runner{Store: NewStore(root)}
}

// Run executes the pipeline, starting at its first declared step and then
// following each step's outcome: normal completion advances to the next
// declared step (Pipeline.stepAfter); a *GotoSignal redirects to any named
// step, forward or backward; a *BreakSignal stops the run outright. When
// resume is true and a valid checkpoint exists, execution starts at the
// checkpoint's NextStep instead of the first step, and the
// already-completed steps are neither re-executed nor reported as executed
// (RF-6, QR-3, AC-7). On full completion the checkpoint is cleared (RF-5,
// AC-6); on break, it is left in place — see BreakSignal's doc comment.
//
// init (may be nil) seeds ctx.Vars before the first step of a *fresh* run
// only — a resumed run restores ctx.Vars from the checkpoint instead, the
// same way it restores current from NextStep rather than recomputing
// either from scratch. Called once per Run() invocation, so pipeline-level
// state resets every time Run() is called anew — once per loop iteration
// when LoopRunner drives it (loop.go), or once for a plain, non-looped
// pipeline.
func (r *Runner) Run(p Pipeline, init InitFunc, exec StepFunc, resume bool) (*RunResult, error) {
	result := &RunResult{}
	instanceID := p.InstanceID
	if instanceID == "" {
		instanceID = "default"
	}
	ctx := &RunContext{Vars: map[string]any{}, InstanceID: instanceID}

	perStep := p.Checkpoint.Enabled && p.Checkpoint.Strategy == "per_step"

	current, ok := p.firstStep()
	resumedFromCheckpoint := false
	if resume && p.Checkpoint.Enabled {
		cp, found, err := r.Store.Load(p.Name)
		if err != nil {
			return result, err
		}
		if found {
			if cp.NextStep == "" {
				// The prior run had already reached its final step.
				return result, nil
			}
			current, ok = cp.NextStep, true
			if cp.Variables != nil {
				ctx.Vars = cp.Variables
			}
			result.Resumed = true
			result.Skipped = append([]string{}, cp.CompletedSteps...)
			resumedFromCheckpoint = true
		}
	}
	if !resumedFromCheckpoint && init != nil {
		if err := init(ctx); err != nil {
			return result, err
		}
	}

	visits := map[string]int{}
	for ok {
		visits[current]++
		if visits[current] > maxStepVisits {
			return result, fmt.Errorf("runtime: step %q revisited more than %d times (a goto cycle with no break?)", current, maxStepVisits)
		}

		err := exec(current, ctx)
		result.Executed = append(result.Executed, current)

		var brk *BreakSignal
		var gt *GotoSignal
		switch {
		case errors.As(err, &brk):
			if perStep {
				cp := &Checkpoint{
					Pipeline:       p.Name,
					LastStep:       current,
					NextStep:       "",
					CompletedSteps: append([]string{}, result.Executed...),
					Variables:      copyVars(ctx.Vars),
					TTLSeconds:     int64(p.Checkpoint.TTL.Seconds()),
				}
				if saveErr := r.Store.Save(cp); saveErr != nil {
					return result, saveErr
				}
			}
			result.Broke, result.BreakReason = true, brk.Reason
			return result, nil

		case errors.As(err, &gt):
			if !p.hasStep(gt.Target) {
				return result, fmt.Errorf("runtime: goto target %q is not a step of pipeline %q", gt.Target, p.Name)
			}
			if err := r.checkpointStep(p, perStep, current, gt.Target, result.Executed, ctx); err != nil {
				return result, err
			}
			current, ok = gt.Target, true

		case err != nil:
			// The prior step's checkpoint (if any) is already persisted;
			// surface the failure so a later --resume can continue here.
			return result, fmt.Errorf("runtime: step %q failed: %w", current, err)

		default:
			next, hasNext := p.stepAfter(current)
			if err := r.checkpointStep(p, perStep, current, next, result.Executed, ctx); err != nil {
				return result, err
			}
			current, ok = next, hasNext
		}
	}

	// Successful completion clears checkpoint state (checkpoint.clear()).
	if p.Checkpoint.Enabled {
		if err := r.Store.Clear(p.Name); err != nil {
			return result, err
		}
	}
	return result, nil
}

// checkpointStep persists NextStep exactly as Run just resolved it — before
// current is reassigned — so a crash between now and the next iteration
// resumes down the same branch this run actually took (see Checkpoint's doc
// comment).
func (r *Runner) checkpointStep(p Pipeline, perStep bool, current, next string, executed []string, ctx *RunContext) error {
	if !perStep {
		return nil
	}
	cp := &Checkpoint{
		Pipeline:       p.Name,
		LastStep:       current,
		NextStep:       next,
		CompletedSteps: append([]string{}, executed...),
		Variables:      copyVars(ctx.Vars),
		TTLSeconds:     int64(p.Checkpoint.TTL.Seconds()),
	}
	return r.Store.Save(cp)
}

func copyVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
