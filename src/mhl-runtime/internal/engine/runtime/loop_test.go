package runtime_test

import (
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/engine/runtime"
)

// oneStepPipeline is the minimal loop pipeline a LoopRunner repeats — its
// own per-step checkpoint config is irrelevant to LoopRunner, which always
// runs it with resume=false (see LoopRunner.Run's doc comment): the
// pipeline's own loop checkpoint, not its per-step one, is what tracks
// progress across iterations.
func oneStepPipeline() runtime.Pipeline {
	return runtime.Pipeline{Name: "Cycle", Steps: []string{"Only"}, Loop: true}
}

// The loop stops as soon as evalStopWhen reports true — checked only after
// a full iteration completes — and not one iteration later.
func TestLoopStopsOnStopWhen(t *testing.T) {
	root := t.TempDir()
	lr := runtime.NewLoopRunner(root)

	calls := 0
	res, err := lr.Run(oneStepPipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		calls++
		return nil
	}, func(string) (bool, error) {
		return calls >= 3, nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TerminalReason != "stop_when" {
		t.Fatalf("TerminalReason = %q, want stop_when", res.TerminalReason)
	}
	if res.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", res.Iterations)
	}
}

// max_iterations is a hard ceiling: it stops the loop even when stop_when
// would never fire on its own.
func TestLoopStopsOnMaxIterations(t *testing.T) {
	root := t.TempDir()
	p := oneStepPipeline()
	p.MaxIterations = 5
	lr := runtime.NewLoopRunner(root)

	res, err := lr.Run(p, nil, func(step string, ctx *runtime.RunContext) error {
		return nil
	}, func(string) (bool, error) {
		return false, nil // never satisfied on its own
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TerminalReason != "max_iterations" || res.Iterations != 5 {
		t.Fatalf("result = %+v, want max_iterations at 5", res)
	}
}

// An explicit `break` inside an iteration wins over both stop_when and
// max_iterations: the loop stops immediately, evalStopWhen is never called
// for that iteration, and the break's reason is preserved.
func TestLoopStopsOnBreakBeforeEvaluatingStopWhen(t *testing.T) {
	root := t.TempDir()
	p := oneStepPipeline()
	p.MaxIterations = 100
	lr := runtime.NewLoopRunner(root)

	stopWhenCalls := 0
	res, err := lr.Run(p, nil, func(step string, ctx *runtime.RunContext) error {
		return &runtime.BreakSignal{Reason: "gave up"}
	}, func(string) (bool, error) {
		stopWhenCalls++
		return false, nil
	}, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TerminalReason != "break" || res.BreakReason != "gave up" {
		t.Fatalf("result = %+v, want break/\"gave up\"", res)
	}
	if stopWhenCalls != 0 {
		t.Errorf("evalStopWhen called %d times, want 0 — break must win first", stopWhenCalls)
	}
}

// A genuine error from an iteration is not a soft stop: it propagates
// straight out, the same way a plain (non-looped) pipeline's error already
// does — it must not be reported as stop_when/max_iterations/break.
func TestLoopPropagatesGenuineErrors(t *testing.T) {
	root := t.TempDir()
	lr := runtime.NewLoopRunner(root)

	boom := errSimulatedCrash
	_, err := lr.Run(oneStepPipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		return boom
	}, func(string) (bool, error) {
		return false, nil
	}, false)
	if err == nil {
		t.Fatal("expected the genuine error to propagate")
	}
}

// --resume continues at the iteration after the last one that completed
// successfully — not iteration 0 — when a prior run was interrupted by a
// genuine error mid-loop.
func TestLoopResumeContinuesAtNextIteration(t *testing.T) {
	root := t.TempDir()

	lr := runtime.NewLoopRunner(root)
	calls := 0
	_, err := lr.Run(oneStepPipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		calls++
		if calls == 3 {
			return errSimulatedCrash // "crash" during the 3rd iteration
		}
		return nil
	}, func(string) (bool, error) {
		return false, nil // never stops on its own — only the crash ends this run
	}, false)
	if err == nil {
		t.Fatal("expected the simulated crash to propagate")
	}
	// Iterations 1 and 2 completed and were checkpointed; iteration 3 never
	// finished, so the loop checkpoint should record NextIteration = 2.

	lr2 := runtime.NewLoopRunner(root)
	var resumedIterationStarts []int
	iteration := 0
	res, err := lr2.Run(oneStepPipeline(), nil, func(step string, ctx *runtime.RunContext) error {
		iteration++
		resumedIterationStarts = append(resumedIterationStarts, iteration)
		return nil
	}, func(string) (bool, error) {
		return iteration >= 1, nil // stop right after the first resumed iteration
	}, true)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !res.Resumed {
		t.Error("expected Resumed = true")
	}
	// The exec closure itself only ran once in this resumed process — proof
	// the resumed run started its loop body at iteration 2, not iteration 0
	// (a naive resume-from-scratch would have called it 3 times: two to
	// replay what was already done, then the one that finally satisfies
	// stop_when).
	if len(resumedIterationStarts) != 1 {
		t.Fatalf("exec ran %d times after resume, want exactly 1 (no replay of the 2 already-completed iterations)", len(resumedIterationStarts))
	}
	// Iterations reports the running total (2 already done + this 1), not a
	// per-resume count.
	if res.Iterations != 3 {
		t.Fatalf("Iterations after resume = %d, want 3 (2 already done + 1 new)", res.Iterations)
	}
}
