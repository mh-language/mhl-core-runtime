package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

func runTestFile(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "suite.mh")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	err := cli.Run([]string{"test", file}, &buf)
	return buf.String(), err
}

// MHL-Melhorias.md #13: a test's describe block can now run a whole
// pipeline/workflow (steps, goto, break, pause, fail()) via
// `Name.run(inputs: {...})`, closing the gap where the only way to
// exercise a workflow's control flow was a real `mhl run` inspected
// manually. `.run()` returns a plain object — {ok, state, executed, vars,
// error, step, break_reason, pause_reason} — never raises for an outcome
// the workflow's own steps produced (fail()/break/pause), so a test can
// assert on a workflow expected to fail exactly as easily as one expected
// to succeed.
func TestRunWorkflowFollowsGotoAndReportsExecutedSteps(t *testing.T) {
	out, err := runTestFile(t, `
workflow Recascade {
    step Start {
        goto Target
    }
    step Skip {
        var unreachable = true
    }
    step Target {
        var reached = true
    }
}

test t {
    describe goto_redirects {
        var result = Recascade.run()
        are_equal(result.state, "completed")
        are_equal(result.executed, ["Start", "Target"])
        is_true(result.ok)
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "3 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestRunWorkflowReportsBreak(t *testing.T) {
	out, err := runTestFile(t, `
pipeline GuardedRetry {
    step Implement {
        var attempts = 4
        if (attempts > 3) {
            break "too many attempts"
        }
    }
    step Handoff {
        var done = true
    }
}

test t {
    describe break_stops_early {
        var result = GuardedRetry.run()
        are_equal(result.state, "broke")
        are_equal(result.break_reason, "too many attempts")
        are_equal(result.executed, ["Implement"])
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "3 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestRunWorkflowReportsPause(t *testing.T) {
	out, err := runTestFile(t, `
workflow ApprovalGate {
    step Ask {
        pause("awaiting approval")
    }
    step Done {
        var x = 1
    }
}

test t {
    describe pause_suspends {
        var result = ApprovalGate.run()
        are_equal(result.state, "paused")
        are_equal(result.pause_reason, "awaiting approval")
        are_equal(result.executed, ["Ask"])
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "3 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestRunPipelineCompleteStopsBeforeLaterSteps is complete()'s core
// contract: it ends the run in the normal "completed" state right where
// it's called, not just "early-exits this step" the way a bare `return`
// does (return is swallowed by RunStep and still falls through to the next
// step — see interpreter.completeSignal's doc comment). A plain `pipeline`
// (not workflow) proves this doesn't depend on `goto` being available at
// all — complete() is a general primitive, not workflow-specific.
func TestRunPipelineCompleteStopsBeforeLaterSteps(t *testing.T) {
	out, err := runTestFile(t, `
pipeline Demo {
    var reached_b = false
    step A {
        complete()
    }
    step B {
        reached_b = true
    }
}

test t {
    describe complete_ends_the_run_here {
        var result = Demo.run()
        is_true(result.ok)
        are_equal(result.state, "completed")
        are_equal(result.executed, ["A"])
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "3 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// A supplied input overrides its default, and an omitted defaulted input
// falls back to it — the same contract execsvc.coerceInputs/ValidateInputs
// give a real `mhl run` (MHL-Melhorias.md #8), exercised here through the
// test-only .run() path.
func TestRunWorkflowAppliesInputDefault(t *testing.T) {
	out, err := runTestFile(t, `
pipeline WorkItem {
    input action: string
    input item_type: string = "unspecified"
    var seen_type = ""
    step Validate {
        seen_type = item_type
    }
}

test t {
    describe defaults {
        var result = WorkItem.run(inputs: {action: "list"})
        is_true(result.ok)
        are_equal(result.vars["seen_type"], "unspecified")
    }
    describe overrides {
        var result = WorkItem.run(inputs: {action: "create", item_type: "bug"})
        are_equal(result.vars["seen_type"], "bug")
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "3 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// fail() inside a step is reported through the result object (ok=false,
// state="failed", step=<the failing step>, error containing the message),
// not raised — a test asserting a workflow *should* fail doesn't need
// try/catch.
func TestRunWorkflowReportsStepFailure(t *testing.T) {
	out, err := runTestFile(t, `
pipeline WorkItem {
    input action: string
    step Fail {
        if (action == "boom") {
            fail("boom requested")
        }
    }
}

test t {
    describe fail_reports_failure {
        var result = WorkItem.run(inputs: {action: "boom"})
        is_false(result.ok)
        are_equal(result.state, "failed")
        are_equal(result.step, "Fail")
        is_true(result.error.contains("boom requested"))
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "4 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// A missing required input is a problem with the *call*, not an outcome
// the pipeline's own steps produced — it raises a catchable error rather
// than a {ok:false} result, mirroring execsvc's InvalidInputsError being a
// distinct failure class from a runtime.StepError.
func TestRunWorkflowMissingRequiredInputRaises(t *testing.T) {
	out, err := runTestFile(t, `
pipeline WorkItem {
    input action: string
    step S { var x = action }
}

test t {
    describe invalid_inputs_raise {
        var caught = ""
        try {
            var result = WorkItem.run(inputs: {})
        } catch (e) {
            caught = "${e}"
        }
        is_true(caught.contains("action"))
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "1 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// A `loop pipeline`/`loop workflow` is explicitly not supported yet — a
// clear, catchable error rather than mysterious behavior.
func TestRunWorkflowLoopPipelineRaisesClearError(t *testing.T) {
	out, err := runTestFile(t, `
loop pipeline Poll {
    mem attempts = 0
    repeat: { max_iterations: 3 }
    step Check {
        attempts = attempts + 1
    }
}

test t {
    describe loop_not_supported {
        var caught = ""
        try {
            var result = Poll.run()
        } catch (e) {
            caught = "${e}"
        }
        is_true(caught.contains("loop"))
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "1 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// A `parallel` group's branch steps checkpoint concurrently — this exists
// mainly to run under `-race` and prove the in-memory StateStore
// runWorkflowForTest hands the Runner (memStateStore, test_run.go) is safe
// for that.
func TestRunWorkflowWithParallelGroup(t *testing.T) {
	out, err := runTestFile(t, `
pipeline Gather {
    var a = 0
    var b = 0
    var total = 0
    parallel Fetch {
        step A { a = 1 }
        step B { b = 2 }
    }
    step Done {
        total = a + b
    }
}

test t {
    describe parallel_group_completes {
        var result = Gather.run()
        is_true(result.ok)
        are_equal(result.vars["total"], 3)
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "2 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// `.run()` on a declared pipeline/workflow is only meaningful inside a
// test's describe block — calling it from a real pipeline step (outside
// any test) is a clear error, not silent nonsense or a "run inside a run".
func TestRunWorkflowOutsideTestIsRejected(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.mh")
	// Outer declared first: `mhl run` with no explicit workflow name runs
	// the first declared pipeline.
	src := `
pipeline Outer {
    step S {
        var result = Inner.run()
    }
}

pipeline Inner {
    step S { var x = 1 }
}
`
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	err := cli.Run([]string{"run", file}, &buf)
	if err == nil {
		t.Fatalf("expected an error, got success: %s", buf.String())
	}
	if !strings.Contains(err.Error(), "describe block") {
		t.Errorf("unexpected error: %v", err)
	}
}
