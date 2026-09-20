package execsvc_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// TestPipelineSessionStartAndEndFireInOrder proves session_start fires once
// before the first step, session_end fires once after the last, and
// session_start's `resumed` is false on a fresh run.
func TestPipelineSessionStartAndEndFireInOrder(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    session_start: (session) -> { log("SESSION_START " + json.stringify(session)) }
    session_end: (session) -> { log("SESSION_END " + json.stringify(session)) }
    step_start: (step) -> { log("STEP_START " + json.stringify(step)) }
    step Work { log("WORK") }
}
`)
	var out bytes.Buffer
	if _, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out}); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	iStart, iStep, iWork, iEnd := strings.Index(got, "SESSION_START"), strings.Index(got, "STEP_START"), strings.Index(got, "WORK\n"), strings.Index(got, "SESSION_END")
	if iStart < 0 || iStep < 0 || iWork < 0 || iEnd < 0 {
		t.Fatalf("missing an expected marker, got:\n%s", got)
	}
	if !(iStart < iStep && iStep < iWork && iWork < iEnd) {
		t.Fatalf("hooks fired out of order, got:\n%s", got)
	}
	if !strings.Contains(got[iStart:iStep], `"resumed":false`) {
		t.Errorf("want session.resumed=false on a fresh run, got:\n%s", got)
	}
}

// TestPipelineSessionStartFiresOnResume proves session_start fires again on a
// --resume, this time with resumed:true.
func TestPipelineSessionStartFiresOnResume(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline Gate {
    checkpoint: { enabled: true, strategy: "per_step" }
    input approved: string
    session_start: (session) -> { log("SESSION_START " + json.stringify(session)) }
    step Gate { if (approved != "yes") { pause("wait") } }
    step Finish { log("FINISH") }
}
`)
	var first bytes.Buffer
	res, err := execsvc.Run(execsvc.Request{Source: src, Inputs: map[string]any{"approved": "no"}, BaseDir: dir, Out: &first})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !res.Paused {
		t.Fatalf("expected the first run to pause, got: %+v", res)
	}
	if !strings.Contains(first.String(), `"resumed":false`) {
		t.Errorf("want resumed=false on the first run, got:\n%s", first.String())
	}

	var second bytes.Buffer
	res2, err := execsvc.Run(execsvc.Request{Source: src, Inputs: map[string]any{"approved": "yes"}, BaseDir: dir, Resume: true, Out: &second})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res2.Paused {
		t.Fatalf("expected the resumed run to finish, got: %+v", res2)
	}
	if !strings.Contains(second.String(), `"resumed":true`) {
		t.Errorf("want resumed=true on --resume, got:\n%s", second.String())
	}
}

// TestPipelineStepStartEndFirePerStep proves step_start/step_end fire once
// per step, in order, with the right index/total.
func TestPipelineStepStartEndFirePerStep(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    step_start: (step) -> { log("START " + json.stringify(step)) }
    step_end: (step) -> { log("END " + json.stringify(step)) }
    step A { log("RAN:A") }
    step B { log("RAN:B") }
    step C { log("RAN:C") }
}
`)
	var out bytes.Buffer
	if _, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out}); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	for _, step := range []string{"A", "B", "C"} {
		if !strings.Contains(got, `"step":"`+step+`"`) {
			t.Errorf("missing step_start/step_end payload for %s, got:\n%s", step, got)
		}
	}
	if !strings.Contains(got, `"index":1,`) || !strings.Contains(got, `"index":2,`) || !strings.Contains(got, `"index":3,`) {
		t.Errorf("missing expected step indices, got:\n%s", got)
	}
	if !strings.Contains(got, `"total":3`) {
		t.Errorf("want total:3 in every step payload, got:\n%s", got)
	}
	// Order: START A, RAN:A, END A, START B, RAN:B, END B, START C, RAN:C, END C.
	order := []string{
		`START {"error":null,"index":1,"pipeline":"P","step":"A","total":3}`, "RAN:A",
		`END {"error":null,"index":1,"pipeline":"P","step":"A","total":3}`,
		`START {"error":null,"index":2,"pipeline":"P","step":"B","total":3}`, "RAN:B",
		`END {"error":null,"index":2,"pipeline":"P","step":"B","total":3}`,
		`START {"error":null,"index":3,"pipeline":"P","step":"C","total":3}`, "RAN:C",
		`END {"error":null,"index":3,"pipeline":"P","step":"C","total":3}`,
	}
	last := -1
	for _, marker := range order {
		i := strings.Index(got, marker)
		if i < 0 {
			t.Fatalf("missing marker %q, got:\n%s", marker, got)
		}
		if i < last {
			t.Fatalf("marker %q out of order, got:\n%s", marker, got)
		}
		last = i
	}
}

// TestPipelineStepHooksFireForEachParallelBranch proves step_start/step_end
// fire once per branch of a `parallel` group — order between branches is
// non-deterministic, so this asserts presence (a multiset), not sequence.
func TestPipelineStepHooksFireForEachParallelBranch(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    step_start: (step) -> { log("START " + step.step) }
    step_end: (step) -> { log("END " + step.step) }
    parallel Fanout {
        step X { log("X") }
        step Y { log("Y") }
        step Z { log("Z") }
    }
}
`)
	var out bytes.Buffer
	if _, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out}); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	for _, step := range []string{"X", "Y", "Z"} {
		if !strings.Contains(got, "START "+step) || !strings.Contains(got, "END "+step) {
			t.Errorf("missing step_start/step_end for branch %s, got:\n%s", step, got)
		}
	}
}

// TestPipelineStepHooksFireEveryLoopIteration proves step_start/step_end fire
// once per iteration of a `loop pipeline`, while session_start/session_end
// fire exactly once for the whole run (not once per iteration) — and that a
// loop exhausting its `max_iterations` ceiling is a soft stop that fires
// session_end, never stop_failure (the max_iterations/max_step_visits
// naming trap).
func TestPipelineStepHooksFireEveryLoopIterationSessionHooksFireOnce(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
loop pipeline P max 3 {
    mem n = 0
    session_start: (session) -> { log("SESSION_START") }
    session_end: (session) -> { log("SESSION_END " + json.stringify(session)) }
    stop_failure: (failure) -> { log("STOP_FAILURE " + json.stringify(failure)) }
    step_start: (step) -> { log("STEP_START") }
    step_end: (step) -> { log("STEP_END") }
    step Tick { n = n + 1 }
}
`)
	var out bytes.Buffer
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TerminalReason != "max_iterations" {
		t.Fatalf("TerminalReason = %q, want max_iterations", res.TerminalReason)
	}
	got := out.String()
	if n := strings.Count(got, "SESSION_START"); n != 1 {
		t.Errorf("SESSION_START fired %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "SESSION_END"); n != 1 {
		t.Errorf("SESSION_END fired %d times, want 1:\n%s", n, got)
	}
	if strings.Contains(got, "STOP_FAILURE") {
		t.Errorf("stop_failure must not fire on a max_iterations stop, got:\n%s", got)
	}
	if n := strings.Count(got, "STEP_START"); n != 3 {
		t.Errorf("STEP_START fired %d times, want 3 (once per iteration):\n%s", n, got)
	}
	if n := strings.Count(got, "STEP_END"); n != 3 {
		t.Errorf("STEP_END fired %d times, want 3:\n%s", n, got)
	}
}

// TestPipelineSessionEndFiresOnBreak proves `break` is reported to
// session_end (broke:true), and never routes through stop_failure.
func TestPipelineSessionEndFiresOnBreak(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    session_end: (session) -> { log("SESSION_END " + json.stringify(session)) }
    stop_failure: (failure) -> { log("STOP_FAILURE") }
    step Work { break "enough" }
}
`)
	var out bytes.Buffer
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Broke {
		t.Fatalf("expected the run to report Broke, got: %+v", res)
	}
	got := out.String()
	if !strings.Contains(got, `"broke":true`) {
		t.Errorf("want session.broke=true, got:\n%s", got)
	}
	if strings.Contains(got, "STOP_FAILURE") {
		t.Errorf("stop_failure must not fire on break, got:\n%s", got)
	}
}

// TestPipelineHooksDoNotFireOnPause proves pause() suspends the run without
// firing either session_end or stop_failure — it isn't finished, in either
// direction.
func TestPipelineHooksDoNotFireOnPause(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    session_end: (session) -> { log("SESSION_END") }
    stop_failure: (failure) -> { log("STOP_FAILURE") }
    step Gate { pause("wait") }
}
`)
	var out bytes.Buffer
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Paused {
		t.Fatalf("expected the run to pause, got: %+v", res)
	}
	got := out.String()
	if strings.Contains(got, "SESSION_END") || strings.Contains(got, "STOP_FAILURE") {
		t.Errorf("neither hook should fire on pause, got:\n%s", got)
	}
}

// TestPipelineStopFailureFiresOnGenuineError proves a `fail()`'d step routes
// to stop_failure with reason "failed", and never to session_end.
func TestPipelineStopFailureFiresOnGenuineError(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    session_end: (session) -> { log("SESSION_END") }
    stop_failure: (failure) -> { log("STOP_FAILURE " + json.stringify(failure)) }
    step Work { fail("boom") }
}
`)
	var out bytes.Buffer
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err == nil {
		t.Fatal("expected the run to fail")
	}
	got := out.String()
	if !strings.Contains(got, `"reason":"failed"`) {
		t.Errorf("want failure.reason=failed, got:\n%s", got)
	}
	if !strings.Contains(got, `"step":"Work"`) {
		t.Errorf("want failure.step=Work, got:\n%s", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("want the original error message in the payload, got:\n%s", got)
	}
	if strings.Contains(got, "SESSION_END") {
		t.Errorf("session_end must not fire on a genuine failure, got:\n%s", got)
	}
}

// TestPipelineStopFailureFiresOnStepTimeout proves an unrecovered `timeout`
// routes to stop_failure with reason "timeout".
func TestPipelineStopFailureFiresOnStepTimeout(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    stop_failure: (failure) -> { log("STOP_FAILURE " + json.stringify(failure)) }
    step Slow timeout 200ms {
        var r = cmd.exec(["sleep", "30"])
    }
}
`)
	var out bytes.Buffer
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err == nil {
		t.Fatal("expected the step timeout to fail the run")
	}
	got := out.String()
	if !strings.Contains(got, `"reason":"timeout"`) {
		t.Errorf("want failure.reason=timeout, got:\n%s", got)
	}
}

// TestPipelineStopFailureFiresOnMaxStepVisits proves a `goto` cycle with no
// `break` routes to stop_failure with reason "max_step_visits" — distinct
// from a loop's max_iterations soft stop (TestPipelineStepHooksFireEveryLoopIterationSessionHooksFireOnce).
func TestPipelineStopFailureFiresOnMaxStepVisits(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
workflow P {
    stop_failure: (failure) -> { log("STOP_FAILURE " + json.stringify(failure)) }
    step A { goto A }
}
`)
	var out bytes.Buffer
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err == nil {
		t.Fatal("expected the goto cycle to fail the run")
	}
	got := out.String()
	if !strings.Contains(got, `"reason":"max_step_visits"`) {
		t.Errorf("want failure.reason=max_step_visits, got:\n%s", got)
	}
}

// TestPipelineStopFailureFiresOnCancellation proves a cancelled run context
// routes to stop_failure with reason "cancelled".
func TestPipelineStopFailureFiresOnCancellation(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    stop_failure: (failure) -> { log("STOP_FAILURE " + json.stringify(failure)) }
    step A { log("a") }
    step B { var r = cmd.exec(["sleep", "30"]) }
    step C { log("c") }
}
`)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out, Context: ctx})
	if err == nil {
		t.Fatal("expected the cancelled run to fail")
	}
	got := out.String()
	if !strings.Contains(got, `"reason":"cancelled"`) {
		t.Errorf("want failure.reason=cancelled, got:\n%s", got)
	}
}

// TestPipelineHookWithWrongParamCountIsRejected proves the hook signature is
// fixed at exactly one parameter.
func TestPipelineHookWithWrongParamCountIsRejected(t *testing.T) {
	for name, decl := range map[string]string{
		"zero params": `session_start: () -> { }`,
		"two params":  `session_start: (a, b) -> { }`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := writeFile(t, dir, "main.mh", `
pipeline P {
    `+decl+`
    step S { log("hi") }
}
`)
			_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
			if err == nil || !strings.Contains(err.Error(), "takes exactly one parameter") {
				t.Fatalf("expected a one-parameter error, got: %v", err)
			}
		})
	}
}

// TestPipelineHookReturningValueIsRejected proves a hook's return value is
// rejected — lifecycle hooks are for side effects only.
func TestPipelineHookReturningValueIsRejected(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    step_end: (step) -> { return 1 }
    step S { log("hi") }
}
`)
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
	if err == nil || !strings.Contains(err.Error(), "must not return a value") {
		t.Fatalf("expected a return-value error, got: %v", err)
	}
}

// TestStopFailureHookErrorDoesNotMaskOriginalFailure proves a broken
// stop_failure hook body never replaces the pipeline's own failure — it's
// only ever logged alongside it.
func TestStopFailureHookErrorDoesNotMaskOriginalFailure(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    stop_failure: (failure) -> { return "not allowed" }
    step Work { fail("boom") }
}
`)
	var out bytes.Buffer
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the original fail(\"boom\") to be the reported error, got: %v", err)
	}
	if !strings.Contains(out.String(), "warning:") || !strings.Contains(out.String(), "stop_failure") {
		t.Errorf("expected the stop_failure hook's own error to be logged, got:\n%s", out.String())
	}
}

// TestPipelineWithoutHooksIsUnaffected proves a pipeline declaring none of
// the 5 hooks behaves exactly as before this feature existed.
func TestPipelineWithoutHooksIsUnaffected(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    var greeting = ""
    step S { greeting = "hello" }
}
`)
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vars["greeting"] != "hello" {
		t.Errorf("Vars[greeting] = %v, want hello", res.Vars["greeting"])
	}
}
