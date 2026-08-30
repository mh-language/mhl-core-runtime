package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
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
	// SessionID is the per-execution id of the directory this run's
	// checkpoints live under (Runner.SessionID / Store.Session). Empty for an
	// unscoped runner. cli.go exposes it to a pipeline's `context:` element as
	// context.session_id.
	SessionID string
	// Out, when non-nil, is the writer this step's output must go to instead
	// of cli.go's exec closure's default — set by runParallelStage to a
	// per-branch buffer so concurrent branch steps never interleave on the
	// real stdout; their buffers are flushed in declared order once the
	// group joins. nil everywhere else (the closure uses its own writer).
	Out io.Writer
}

// StepFunc executes a single step. Returning a plain error aborts the
// pipeline; the last successfully completed step remains checkpointed for a
// later --resume. Returning a *BreakSignal or *GotoSignal instead (see
// signal.go) is not a failure — execsvc's exec closure produces these from
// interpreter.IsBreak/IsGoto — and Run reacts to each differently, per their
// doc comments.
//
// ctx is the run's context.Context: Run checks it at every step boundary and
// aborts the run (without checkpointing further) once it is done, and passes
// it here so a step implementation can thread it into whatever it blocks on.
// A step already in flight is not interrupted — cancellation takes effect
// when it returns.
type StepFunc func(ctx context.Context, step string, rc *RunContext) error

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
// BreakReason carrying whatever value (if any) it evaluated. FinalVars is
// the accumulated variable state as it stood when the run finished — set on
// normal completion (before the checkpoint is cleared) so cli.go can persist
// it as this session's result.json for a later run's `context:` to read;
// nil when the run broke or failed.
type RunResult struct {
	Executed    []string
	Skipped     []string
	Resumed     bool
	Broke       bool
	BreakReason any
	FinalVars   map[string]any
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
	// SessionID is the per-execution id whose directory Store is scoped to
	// (see Store.Session), surfaced on RunContext.SessionID and used by
	// cli.go's `context:` support. Empty for an unscoped runner — the legacy
	// single-file layout, still used by a --resume that fell back to a
	// pre-session checkpoint and by tests that construct a Runner directly.
	SessionID string
	// Out is where a `parallel` group's per-branch captured output is
	// flushed, in declared order, once the group joins — the Runner
	// otherwise never writes step output itself (cli.go's exec closure
	// does). nil is tolerated (output is discarded); a pipeline with no
	// `parallel` group never reaches this path at all.
	Out io.Writer
}

// NewRunner returns a Runner backed by a Store rooted at root. It is
// unscoped — call Session to isolate one execution's checkpoints.
func NewRunner(root string) *Runner {
	return &Runner{Store: NewStore(root)}
}

// Session returns a copy of r whose Store writes under a per-execution
// directory named id, so two concurrent runs of the same pipeline never
// clobber each other's checkpoint. An empty id returns an unscoped runner.
func (r *Runner) Session(id string) *Runner {
	return &Runner{Store: r.Store.Session(id), SessionID: id, Out: r.Out}
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
func (r *Runner) Run(runCtx context.Context, p Pipeline, init InitFunc, exec StepFunc, resume bool) (*RunResult, error) {
	if runCtx == nil {
		runCtx = context.Background()
	}
	result := &RunResult{}
	// Opportunistic housekeeping: drop long-abandoned session directories
	// left by runs that were hard-killed and never resumed. Best-effort and
	// bounded (see PruneExpired) so it never slows a run down noticeably.
	if r.SessionID != "" {
		_ = PruneExpired(r.Store.base)
	}
	instanceID := p.InstanceID
	if instanceID == "" {
		instanceID = "default"
	}
	ctx := &RunContext{Vars: map[string]any{}, InstanceID: instanceID, SessionID: r.SessionID, Out: r.Out}

	// PipelineFromAST fills Stages, but a Pipeline built as a literal (tests,
	// and any caller that only sets Steps) may not — derive one singleton
	// stage per step so the stage walk below is the single code path.
	if len(p.Stages) == 0 {
		p.Stages = make([]Stage, len(p.Steps))
		for i, s := range p.Steps {
			p.Stages[i] = Stage{Name: s, Steps: []string{s}}
		}
	}

	perStep := p.Checkpoint.Enabled && p.Checkpoint.Strategy == "per_step"

	first, ok := p.firstStage()
	current := first.Name
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
		if err := runCtx.Err(); err != nil {
			return result, fmt.Errorf("runtime: run cancelled before step %q: %w", current, err)
		}
		visits[current]++
		if visits[current] > maxStepVisits {
			return result, fmt.Errorf("runtime: step %q revisited more than %d times (a goto cycle with no break?)", current, maxStepVisits)
		}

		stage, found := p.stageByName(current)
		if !found {
			return result, fmt.Errorf("runtime: pipeline %q has no step or parallel group %q", p.Name, current)
		}

		var err error
		if stage.Parallel {
			err = r.runParallelStage(runCtx, p, stage, ctx, exec)
		} else {
			err = exec(runCtx, stage.Steps[0], ctx)
		}
		result.Executed = append(result.Executed, stage.Steps...)

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
			if p.stepInParallelGroup(gt.Target) {
				return result, fmt.Errorf("runtime: goto target %q is a step inside a parallel group of pipeline %q", gt.Target, p.Name)
			}
			if err := r.checkpointStep(p, perStep, current, gt.Target, result.Executed, ctx); err != nil {
				return result, err
			}
			current, ok = gt.Target, true

		case err != nil:
			// The prior stage's checkpoint (if any) is already persisted;
			// surface the failure so a later --resume can continue here.
			return result, fmt.Errorf("runtime: step %q failed: %w", current, err)

		default:
			next, hasNext := p.stageAfter(current)
			if err := r.checkpointStep(p, perStep, current, next.Name, result.Executed, ctx); err != nil {
				return result, err
			}
			current, ok = next.Name, hasNext
		}
	}

	// Capture the final variable state before the checkpoint is cleared, so
	// cli.go can persist it as this session's result.json for a later run's
	// `context:` to read.
	result.FinalVars = copyVars(ctx.Vars)

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

// runParallelStage runs every branch step of a `parallel` group concurrently
// and joins them before returning — the pipeline does not advance past the
// group until all branches finish. Each branch gets its own deep copy of
// ctx.Vars (so branches never race on the shared map) and its own output
// buffer (flushed to r.Out in declared order once the group joins, so
// concurrent branches never interleave on stdout). The join is fail-slow:
// even when one branch errors, the rest are allowed to finish before the
// first error (in declared order) is returned; there is no mid-step
// cancellation — runCtx is passed to each branch's StepFunc but the barrier
// still waits for every branch to return. On success, each branch's writes
// are merged back into ctx.Vars — a key two branches set to different values
// is a hard error, so a parallel group's variable outcome is always
// deterministic.
func (r *Runner) runParallelStage(runCtx context.Context, p Pipeline, stage Stage, ctx *RunContext, exec StepFunc) error {
	base := deepCopyVars(ctx.Vars)
	n := len(stage.Steps)
	bctxs := make([]*RunContext, n)
	bufs := make([]*bytes.Buffer, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i, name := range stage.Steps {
		buf := &bytes.Buffer{}
		bufs[i] = buf
		bctx := &RunContext{
			Vars:       deepCopyVars(ctx.Vars),
			InstanceID: ctx.InstanceID,
			SessionID:  ctx.SessionID,
			Out:        buf,
		}
		bctxs[i] = bctx
		wg.Add(1)
		go func(i int, name string, bctx *RunContext) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					errs[i] = fmt.Errorf("parallel step %q panicked: %v", name, rec)
				}
			}()
			errs[i] = exec(runCtx, name, bctx)
		}(i, name, bctx)
	}
	wg.Wait()

	out := r.Out
	if out == nil {
		out = io.Discard
	}
	for _, buf := range bufs {
		if buf.Len() > 0 {
			_, _ = io.WriteString(out, buf.String())
		}
	}

	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("parallel group %q: step %q: %w", stage.Name, stage.Steps[i], err)
		}
	}

	// writtenBy records which branch first set a given key to a value that
	// differs from the pre-fork snapshot; a second branch setting the same
	// key to a different value is a conflict.
	writtenBy := make(map[string]int, n)
	for i, bctx := range bctxs {
		for k, v := range bctx.Vars {
			if strings.HasPrefix(k, "__") {
				continue // interpreter bookkeeping (e.g. __last_step)
			}
			if reflect.DeepEqual(base[k], v) {
				continue
			}
			if prev, ok := writtenBy[k]; ok {
				if !reflect.DeepEqual(bctxs[prev].Vars[k], v) {
					return fmt.Errorf("parallel group %q: steps %q and %q both assigned pipeline var %q",
						stage.Name, stage.Steps[prev], stage.Steps[i], k)
				}
				continue
			}
			writtenBy[k] = i
			ctx.Vars[k] = v
		}
	}
	ctx.Vars["__last_step"] = stage.Name
	return nil
}

// deepCopyVars clones the JSON-shaped part of a variable map (arrays and
// objects) so a concurrently-running parallel branch reading it cannot
// observe another branch — or the parent — mutating the same structure.
// Scalars are immutable and shared as-is; this mirrors
// interpreter.deepCopyValue, which spawn.go uses for the same reason.
func deepCopyVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			c[i] = deepCopyValue(t[i])
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(t))
		for k := range t {
			c[k] = deepCopyValue(t[k])
		}
		return c
	default:
		return v
	}
}
