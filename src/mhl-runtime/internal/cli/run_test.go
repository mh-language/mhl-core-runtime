package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const pipelineFile = `
pipeline AutoFixPipeline {
    input issue_id: string

    checkpoint: {
        enabled: true
        strategy: "per_step"
        storage: "file"
        ttl: 7d
    }

    step AuditWithSkill {
        var x = 1
    }

    step RefinementLoop {
        var y = 2
    }
}
`

// IF-1: `mhl run <pipeline.mh>` executes a pipeline from the start; a
// subsequent completed run clears its checkpoint.
func TestRunPipelineFromStart(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Run from within dir so .mhl/state is created there.
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--input", "issue_id=BUG-1"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"step: AuditWithSkill", "step: RefinementLoop", "executed 2 step(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Every run gets its own session directory under .mhl/state; a completed
	// pipeline clears its checkpoint and, with nothing else left in it, the
	// session directory too.
	sessionID := sessionIDFromOutput(t, out)
	if _, err := os.Stat(filepath.Join(dir, ".mhl", "state", sessionID)); !os.IsNotExist(err) {
		t.Errorf("expected session dir %s cleaned up after successful run", sessionID)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mhl", "state", "AutoFixPipeline.json")); !os.IsNotExist(err) {
		t.Errorf("no legacy top-level checkpoint should ever be written")
	}
}

// sessionIDFromOutput extracts the id from the "session: <id>" line cli.Run
// prints at the start of every `mhl run`.
func sessionIDFromOutput(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "session: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "session: "))
		}
	}
	t.Fatalf("no 'session:' line in output:\n%s", out)
	return ""
}

func TestRunResumeFlagParsed(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// With no prior checkpoint, --resume simply runs from the start.
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--resume"}, &buf); err != nil {
		t.Fatalf("run --resume: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 2 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestRunMissingFile(t *testing.T) {
	var buf bytes.Buffer
	if err := cli.Run([]string{"run"}, &buf); err == nil {
		t.Fatal("expected usage error when no file given")
	}
}

const gotoBreakPipelineFile = `
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
`

// A step's explicit `goto` redirects the run to a named step instead of
// falling through to the next declared one — end to end, through the same
// interpreter.RunStep -> cli.go closure -> runtime.Runner path a real `mhl
// run` invocation uses, not just the runtime package in isolation.
func TestRunGotoRedirectsToNamedStep(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(gotoBreakPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "step: Skip") {
		t.Errorf("goto should have skipped the Skip step entirely:\n%s", out)
	}
	if !strings.Contains(out, "step: Start") || !strings.Contains(out, "step: Target") {
		t.Errorf("expected Start and Target to both run, got:\n%s", out)
	}
	if !strings.Contains(out, "executed 2 step(s)") {
		t.Errorf("expected exactly 2 steps executed (Start, Target), got:\n%s", out)
	}
}

const breakPipelineFile = `
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
`

// A step's explicit `break` stops the run outright: Handoff never executes,
// and the CLI reports the break and its reason instead of a step count that
// implies normal completion.
func TestRunBreakStopsPipelineWithReason(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(breakPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "step: Handoff") {
		t.Errorf("break should have stopped before Handoff:\n%s", out)
	}
	if !strings.Contains(out, "stopped by break: too many attempts") {
		t.Errorf("expected the break reason in output:\n%s", out)
	}
}

const loopStopWhenFile = `
memory Counter {
    type: "kv"
    store: "memory"
}

loop pipeline Increment {
    repeat: {
        stop_when: Counter.get("n", 0) >= 3
        max_iterations: 10
    }

    step Bump {
        var current = Counter.get("n", 0)
        Counter.set("n", current + 1)
    }
}
`

// A `loop` repeats its referenced pipeline until stop_when is satisfied —
// end to end: real .mh source declaring memory/pipeline/loop together,
// through cli.Run, the same path `mhl run` itself takes.
func TestRunLoopStopsOnStopWhen(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(loopStopWhenFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `loop "Increment" ran 3 iteration(s), stopped: stop_when`) {
		t.Errorf("unexpected output:\n%s", out)
	}
	if strings.Count(out, "step: Bump") != 3 {
		t.Errorf("expected Bump to run exactly 3 times, got:\n%s", out)
	}
}

const loopMaxIterationsFile = `
loop pipeline NoOp {
    repeat: {
        stop_when: false
        max_iterations: 4
    }

    step Idle {
        var x = 1
    }
}
`

// max_iterations is a hard ceiling independent of stop_when.
func TestRunLoopStopsOnMaxIterations(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(loopMaxIterationsFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `loop "NoOp" ran 4 iteration(s), stopped: max_iterations`) {
		t.Errorf("unexpected output:\n%s", out)
	}
}

const pipelineVarSharedAcrossStepsFile = `
pipeline Cycle {
    var pending = ["a", "b", "c"]
    var visits = 0

    step S1 {
        log("S1 pending=${pending} visits=${visits}")
        visits = visits + 1
    }

    step S2 {
        log("S2 pending=${pending} visits=${visits}")
        pending = pending[1..]
        visits = visits + 1
    }

    step S3 {
        log("S3 pending=${pending} visits=${visits}")
    }
}
`

// A pipeline-level `var` (PipelineMember.Var) is shared read/write across
// every step of one run via plain assignment — unlike a step's own `var`,
// which never survives past that one step (RunStep, exec.go).
func TestRunPipelineVarSharedAcrossSteps(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineVarSharedAcrossStepsFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`S1 pending=["a","b","c"] visits=0`,
		`S2 pending=["a","b","c"] visits=1`,
		`S3 pending=["b","c"] visits=2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

const pipelineVarResetsEachLoopIterationFile = `
memory Counter {
    type: "kv"
    store: "memory"
}

loop pipeline Cycle {
    repeat: {
        stop_when: Counter.get("total", 0) >= 3
        max_iterations: 10
    }

    var seen_this_iteration = 0

    step Bump {
        seen_this_iteration = seen_this_iteration + 1
        log("seen_this_iteration=${seen_this_iteration}")

        var total = Counter.get("total", 0)
        Counter.set("total", total + 1)
    }
}
`

// A pipeline var resets to its declared initial value on every fresh
// Runner.Run() — one loop iteration — while `memory` (unlike a pipeline
// var) keeps accumulating across iterations, which is what stop_when
// actually observes. This is the "pipeline var resets each loop iteration"
// behavior chosen over the alternative (persisting for the whole loop).
func TestRunPipelineVarResetsEachLoopIteration(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineVarResetsEachLoopIterationFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if got := strings.Count(out, "seen_this_iteration=1"); got != 3 {
		t.Errorf("expected seen_this_iteration=1 on all 3 iterations (fresh reset each time), got %d occurrences:\n%s", got, out)
	}
	if strings.Contains(out, "seen_this_iteration=2") {
		t.Errorf("pipeline var must not accumulate across iterations:\n%s", out)
	}
	if !strings.Contains(out, `loop "Cycle" ran 3 iteration(s), stopped: stop_when`) {
		t.Errorf("unexpected output:\n%s", out)
	}
}

const pipelineVarReevaluatesFromMemoryFile = `
memory Counter {
    type: "kv"
    store: "memory"
}

loop pipeline Cycle {
    repeat: {
        stop_when: Counter.get("total", 0) >= 3
        max_iterations: 10
    }

    var total_so_far = Counter.get("total", 0)

    step Bump {
        log("total_so_far=${total_so_far}")
        Counter.set("total", total_so_far + 1)
    }
}
`

// A pipeline var's initializing expression re-evaluates fresh at the start
// of every iteration — it must see that iteration's current memory state,
// not a value computed once and reused (which would make it useless for
// anything that reads memory).
func TestRunPipelineVarReevaluatesFromMemoryEachIteration(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineVarReevaluatesFromMemoryFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"total_so_far=0", "total_so_far=1", "total_so_far=2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

const stepVarShadowsPipelineVarFile = `
pipeline Cycle {
    var x = "pipeline value"

    step S1 {
        var x = "step-local value"
        log("S1 sees x=${x}")
    }

    step S2 {
        log("S2 sees x=${x}")
    }
}
`

// A step's own `var x = ...` always writes to the step's local env, never
// to a same-named pipeline var (env is checked before pipelineEnv on both
// the read and the assign side) — so S1's local shadow is invisible to S2,
// which still sees the pipeline value untouched.
func TestRunStepVarShadowsPipelineVarOfSameName(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(stepVarShadowsPipelineVarFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "S1 sees x=step-local value") {
		t.Errorf("output missing step-local shadow:\n%s", out)
	}
	if !strings.Contains(out, "S2 sees x=pipeline value") {
		t.Errorf("pipeline var was overwritten by the step-local shadow:\n%s", out)
	}
}

const pipelineVarUndefinedAssignmentFile = `
pipeline Cycle {
    var known = 1

    step S1 {
        unknown = 2
    }
}
`

// Assigning to a name that's neither a step-local var nor a declared
// pipeline var is still a fail-closed error — pipelineEnv's fallback in
// execAssign doesn't loosen that.
func TestRunAssignToUndeclaredNameStillErrors(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineVarUndefinedAssignmentFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh"}, &buf)
	if err == nil || !strings.Contains(err.Error(), `undefined variable "unknown"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

const pipelineVarVisibleInsideClosureFile = `
pipeline Cycle {
    var threshold = 2

    step S1 {
        var numbers = [1, 2, 3, 4]
        var above = numbers.filter((n) -> n > threshold)
        log("above=${above}")
    }
}
`

// A lambda created inline within a step body (e.g. a filter/sort_by
// predicate) still sees pipeline vars, not just the step's own — invoked
// through invokeClosureWithValues, whose callCtx now carries
// definingCtx.pipelineEnv forward (closure.go).
func TestRunPipelineVarVisibleInsideClosure(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(pipelineVarVisibleInsideClosureFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "above=[3,4]") {
		t.Errorf("output missing %q:\n%s", "above=[3,4]", out)
	}
}

const toolMethodDoesNotSeePipelineVarFile = `
tool T {
    echo_threshold() -> threshold
}

pipeline Cycle {
    var threshold = 2

    step S1 {
        log(T.echo_threshold())
    }
}
`

// A tool method call stays a real function-call boundary that sees only
// its own bound parameters (TestToolMethodDoesNotSeeCallerVariables,
// tool_test.go already covers this for a step's own vars) — pipeline vars
// are no exception: evalToolCall's childCtx (tool.go) never sets
// pipelineEnv.
func TestRunToolMethodDoesNotSeePipelineVar(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(toolMethodDoesNotSeePipelineVarFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh"}, &buf)
	if err == nil || !strings.Contains(err.Error(), `undefined variable "threshold"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

const optionalAccessAndCollectionOpsFile = `
pipeline P {
    step S {
        var comma = ","
        var obj = { profile: { name: "  Ada  " }, retries: 0 }
        var fallback = "unknown"
        var rkey = "retries"
        var mkey = "missing"
        // ?. reads through a present chain, a missing field, and a null hop
        log("name=${obj?.profile?.name?.trim()}")
        log("city=${obj?.address?.city ?? fallback}")
        // ?? keeps a real 0, only null is replaced
        log("retries=${obj?.retries ?? 3}")

        var nums = [3, 1, 2, 2, 4]
        log("mapped=${nums.map((n) -> n * 2).join(comma)}")
        log("sum=${nums.reduce((a, b) -> a + b, 0)}")
        log("any=${nums.any((n) -> n > 3)}")
        log("all=${nums.all((n) -> n > 0)}")
        log("uniq=${nums.unique().join(comma)}")
        log("appended=${nums.append(9).join(comma)}")
        log("eq=${[1,2].equals([1,2])}")
        log("kind=${type_of(nums)}/${is_array(nums)}/${is_string(nums)}")
        log("got=${obj.get(rkey, 7)}/${obj.get(mkey, 7)}")
    }
}
`

// End-to-end coverage of the v0.4.x expression additions: the ?. optional
// member operator, the ?? null-coalescing operator, the new array methods
// (map/reduce/any/all/append/join/unique), object get(key, default), the
// universal equals(), and the bare type_of / is_* introspection builtins.
func TestRunOptionalAccessAndCollectionOps(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(optionalAccessAndCollectionOpsFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"name=Ada",
		"city=unknown",
		"retries=0",
		"mapped=6,2,4,4,8",
		"sum=12",
		"any=true",
		"all=true",
		"uniq=3,1,2,4",
		"appended=3,1,2,2,4,9",
		"eq=true",
		"kind=array/true/false",
		"got=0/7",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
