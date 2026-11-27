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

// `mem` is the run's own state, like a `var`: two separate `mhl run`
// invocations of the same pipeline each start from the initializer, and so
// does a new run that names a finished run's session with `--session` — the
// mem ended with that run (its checkpoint was cleared). Only a `--resume` of
// a run that stopped continues it (TestRunMemRollsBackWithAFailedStep).
func TestRunMemBelongsToTheRun(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memPlainPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := cli.Run(append([]string{"run", "pipeline.mh"}, args...), &buf); err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
		return buf.String()
	}

	for i := 1; i <= 2; i++ {
		if out := run(); !strings.Contains(out, "visits=1") {
			t.Fatalf("independent run %d: expected a fresh mem (visits=1), got:\n%s", i, out)
		}
	}
	for i := 1; i <= 2; i++ {
		if out := run("--session", "fixed"); !strings.Contains(out, "visits=1") {
			t.Fatalf("run %d of session fixed: expected visits=1 (the previous run finished), got:\n%s", i, out)
		}
	}
}

const memRollbackFile = `
pipeline Counter {
    input crash: bool = false
    mem visits = 0

    step Bump {
        visits = visits + 1
        log("visits=${visits}")
        if (crash) { fail("simulated crash after the write") }
    }
}
`

// A mem lives in the checkpoint with the vars, so a step that fails after
// writing one leaves no trace of the write: the resume runs the step again
// from the last completed step's value, and the write happens once. (When
// mem had a file of its own, written on every assignment, the resumed step
// incremented it a second time.) Nothing but the checkpoint is written.
func TestRunMemRollsBackWithAFailedStep(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(memRollbackFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var first bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--session", "s1", "--input", "crash=true"}, &first); err == nil {
		t.Fatalf("expected the simulated crash, got:\n%s", first.String())
	}
	var second bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--session", "s1", "--resume", "--input", "crash=false"}, &second); err != nil {
		t.Fatalf("resume: %v\n%s", err, second.String())
	}
	if !strings.Contains(second.String(), "visits=1") {
		t.Errorf("resumed step should count once (visits=1), got:\n%s", second.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".mhl", "state", "s1", "mem")); !os.IsNotExist(err) {
		t.Errorf("a separate mem directory was written (err=%v)", err)
	}
}
