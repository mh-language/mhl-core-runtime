package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	return dir
}

const memCounterFile = `
loop pipeline MainPipeline {
    mem count = 0

    repeat: {
        stop_when: count == 10
        max_iterations: 10
    }

    step Step1 { count = count + 1 }
    step Step2 { count = count + 1 }
    step Step3 { count = count + 1 }
    step Step4 { count = count + 1 }
    step Step5 {
        count = count + 1
        log("Step 5 executed. Count: ${count}")
    }
}
`

// This is the exact reported bug's shape: a plain `var` counter reset every
// iteration and was invisible to stop_when, so `stop_when: count == 10`
// never fired within a loop of 5-step, single-increment-per-step iterations
// (the counter topped out at 5 every time) and errored "undefined variable
// count" the moment stop_when tried to read it. `mem` fixes both: it
// survives across iterations, and stop_when can read it (EvalCondition,
// condition.go).
func TestRunMemSurvivesLoopIterationsAndVisibleInStopWhen(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memCounterFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `loop "MainPipeline" ran 2 iteration(s), stopped: stop_when`) {
		t.Errorf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "Step 5 executed. Count: 10") {
		t.Errorf("expected count to reach 10 across 2 iterations of 5 increments each:\n%s", out)
	}
}

const memResetPerRunFile = `
loop pipeline Isolated {
    mem count = 0

    repeat: {
        stop_when: false
        max_iterations: 1
    }

    step Bump {
        count = count + 1
        log("count=${count}")
    }
}
`

// Two independent (non-resumed) runs of the same loop pipeline must never
// share `mem` state just because they share a pipeline name — each gets a
// fresh instance id (LoopRunner.Run), so the counter restarts at 1 both
// times, not 1 then 2.
func TestRunMemIsolatedAcrossIndependentRuns(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memResetPerRunFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if !strings.Contains(buf.String(), "count=1") {
			t.Errorf("run %d: expected a fresh instance to start count at 1, got:\n%s", i, buf.String())
		}
	}
}

const memResumeFile = `
memory Progress {
    type: "kv"
    store: "memory"
}

loop pipeline Resumable {
    mem count = 0

    repeat: {
        stop_when: false
        max_iterations: 5
    }

    step Bump {
        var runs = Progress.get("runs", 0)
        Progress.set("runs", runs + 1)
        if (runs + 1 == 2) {
            fail("simulated crash")
        }
        count = count + 1
        log("count=${count}")
    }
}
`

// --resume must recover the SAME instance id (and so the same mem file) the
// interrupted run was using — not start a fresh one — so a `mem` counter
// picks up where it left off instead of resetting. Progress (an ordinary
// process-scoped KV memory) is what deterministically triggers a "crash" on
// the 2nd Bump call of each process, independent of `count` itself, so the
// test can assert on `count` without the crash trigger interfering with it.
func TestRunMemPersistsAcrossResume(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memResumeFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var first bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh"}, &first)
	if err == nil {
		t.Fatalf("expected the simulated crash to propagate on the first run")
	}
	firstOut := first.String()
	if !strings.Contains(firstOut, "count=1") {
		t.Fatalf("expected count=1 before the simulated crash, got:\n%s", firstOut)
	}
	if strings.Contains(firstOut, "count=2") {
		t.Fatalf("count must not have advanced past 1 before the crash:\n%s", firstOut)
	}

	var second bytes.Buffer
	err = cli.Run([]string{"run", "pipeline.mh", "--resume"}, &second)
	if err == nil {
		t.Fatalf("expected the resumed run to hit the simulated crash again")
	}
	secondOut := second.String()
	if !strings.Contains(secondOut, "count=2") {
		t.Fatalf("expected the resumed run to continue from the persisted count=1 to count=2, got:\n%s", secondOut)
	}
}

const memResetMethodFile = `
loop pipeline Ticker {
    mem count = 0

    repeat: {
        stop_when: false
        max_iterations: 3
    }

    step Tick {
        count = count + 1
        log("count=${count}")
        if (count == 2) {
            count.reset()
        }
    }
}
`

// count.reset() deletes the stored value, so the next read/write re-runs
// count's get-or-init initializer — here that means the iteration right
// after the reset starts back at 1, not 3.
func TestRunMemResetReinitializes(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memResetMethodFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	// Iteration 1: count=1. Iteration 2: count=2, then reset() deletes it.
	// Iteration 3: get-or-init re-runs, so count is back to 1, not 3 — "1"
	// appears twice, "2" once, "3" never.
	if got := strings.Count(out, "count=1"); got != 2 {
		t.Errorf("expected count=1 twice (iteration 1, then again after reset in iteration 3), got %d:\n%s", got, out)
	}
	if got := strings.Count(out, "count=2"); got != 1 {
		t.Errorf("expected count=2 exactly once, got %d:\n%s", got, out)
	}
	if strings.Contains(out, "count=3") {
		t.Errorf("count must not reach 3 — reset() should have restarted it:\n%s", out)
	}
}

const memPlainPipelineFile = `
pipeline Counter {
    mem visits = 0

    step Bump {
        visits = visits + 1
        log("visits=${visits}")
    }
}
`

// A plain (non-loop) pipeline's `mem` uses the fixed "default" instance
// (runtime.Runner.Run's fallback for Pipeline.InstanceID) — so, unlike a
// loop pipeline's independent runs, two separate `mhl run` invocations of
// the same non-loop pipeline DO share state: that's the whole value of
// `mem` over `var` for a pipeline that only ever runs once per invocation.
func TestRunMemPersistsAcrossSeparateRunsOfPlainPipeline(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memPlainPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var first bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !strings.Contains(first.String(), "visits=1") {
		t.Fatalf("expected visits=1 on the first run, got:\n%s", first.String())
	}

	var second bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &second); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !strings.Contains(second.String(), "visits=2") {
		t.Fatalf("expected visits=2 on the second run (mem persisted from the first), got:\n%s", second.String())
	}
}

// mem's backing file lives under .mhl/state/mem/<pipeline>/<instance>.json,
// alongside (never colliding with) the checkpoint/loop-checkpoint files
// Store/LoopStore already keep directly under .mhl/state.
func TestRunMemBackingFileLocation(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memPlainPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := filepath.Join(dir, ".mhl", "state", "mem", "Counter", "default.json")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected mem file at %s: %v", want, err)
	}
}
