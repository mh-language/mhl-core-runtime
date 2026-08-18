package runtime

import "fmt"

// RunContext carries the accumulated variable/session state across steps. It is
// persisted into and restored from checkpoints.
type RunContext struct {
	Vars map[string]string
}

// StepFunc executes a single step. Returning an error aborts the pipeline; the
// last successfully completed step remains checkpointed for a later --resume.
type StepFunc func(step string, ctx *RunContext) error

// RunResult reports which steps actually executed and which were skipped by a
// resume.
type RunResult struct {
	Executed []string
	Skipped  []string
	Resumed  bool
}

// Runner executes a pipeline's steps in order, checkpointing per step when the
// pipeline's checkpoint config is enabled with the per_step strategy.
type Runner struct {
	Store *Store
}

// NewRunner returns a Runner backed by a Store rooted at root.
func NewRunner(root string) *Runner {
	return &Runner{Store: NewStore(root)}
}

// Run executes the pipeline. When resume is true and a valid checkpoint exists,
// execution starts at the step following the last completed step and the
// already-completed steps are neither re-executed nor reported as executed
// (RF-6, QR-3, AC-7). On full completion the checkpoint is cleared (RF-5,
// AC-6).
func (r *Runner) Run(p Pipeline, exec StepFunc, resume bool) (*RunResult, error) {
	result := &RunResult{}
	ctx := &RunContext{Vars: map[string]string{}}

	perStep := p.Checkpoint.Enabled && p.Checkpoint.Strategy == "per_step"
	startIndex := 0

	if resume && p.Checkpoint.Enabled {
		cp, ok, err := r.Store.Load(p.Name)
		if err != nil {
			return result, err
		}
		if ok {
			// Continue at the step after the last completed one.
			startIndex = cp.LastStepIndex + 1
			if cp.Variables != nil {
				ctx.Vars = cp.Variables
			}
			result.Resumed = true
			for i := 0; i < startIndex && i < len(p.Steps); i++ {
				result.Skipped = append(result.Skipped, p.Steps[i])
			}
		}
	}

	for i := startIndex; i < len(p.Steps); i++ {
		step := p.Steps[i]
		if err := exec(step, ctx); err != nil {
			// The prior step's checkpoint (if any) is already persisted;
			// surface the failure so a later --resume can continue here.
			return result, fmt.Errorf("runtime: step %q failed: %w", step, err)
		}
		result.Executed = append(result.Executed, step)

		if perStep {
			cp := &Checkpoint{
				Pipeline:       p.Name,
				LastStep:       step,
				LastStepIndex:  i,
				CompletedSteps: append([]string{}, p.Steps[:i+1]...),
				Variables:      copyVars(ctx.Vars),
				TTLSeconds:     int64(p.Checkpoint.TTL.Seconds()),
			}
			if err := r.Store.Save(cp); err != nil {
				return result, err
			}
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

func copyVars(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
