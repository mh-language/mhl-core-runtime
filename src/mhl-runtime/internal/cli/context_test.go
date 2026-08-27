package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// runInDir writes src to <dir>/pipeline.mh, chdir's into dir for the test,
// and returns a helper that runs `mhl run pipeline.mh <extra...>` capturing
// stdout.
func runInDir(t *testing.T, dir, src string) func(extra ...string) (string, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.mh"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	return func(extra ...string) (string, error) {
		var buf bytes.Buffer
		err := cli.Run(append([]string{"run", "pipeline.mh"}, extra...), &buf)
		return buf.String(), err
	}
}

const contextMetadataPipeline = `
pipeline Demo {
    context: {
        source: "latest"
    }

    var n = 0

    step One {
        log("ctx session: " + context.session_id)
        n = 1
    }
}
`

// A `context:` block exposes this run's own session id to its steps as the
// read-only identifier context.session_id.
func TestRunContextExposesSessionMetadata(t *testing.T) {
	run := runInDir(t, t.TempDir(), contextMetadataPipeline)
	out, err := run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	id := sessionIDFromOutput(t, out)
	if !strings.Contains(out, "ctx session: "+id) {
		t.Errorf("context.session_id (%q) not echoed in output:\n%s", id, out)
	}
}

const contextCarryoverPipeline = `
pipeline Demo {
    checkpoint: {
        enabled: true
        strategy: "per_step"
        ttl: 7d
    }
    context: {
        source: "latest"
    }

    var total = 0

    step Accumulate {
        if (context.vars.keys().contains("total")) {
            log("prev total")
            log(context.vars["total"])
        } else {
            log("prev total: none")
        }
        total = 42
    }
}
`

// The second run of a pipeline sees the first run's completed variable state
// through context.vars (persisted as the session's result.json).
func TestRunContextVarsFromPriorRun(t *testing.T) {
	run := runInDir(t, t.TempDir(), contextCarryoverPipeline)

	out1, err := run()
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if !strings.Contains(out1, "prev total: none") {
		t.Errorf("run 1 should see no prior state:\n%s", out1)
	}

	out2, err := run()
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if !strings.Contains(out2, "prev total") || !strings.Contains(out2, "42") {
		t.Errorf("run 2 should see run 1's total=42 via context.vars:\n%s", out2)
	}
}

const contextRequirePipeline = `
pipeline Demo {
    context: {
        require: true
    }

    step Only {
        log("ran")
    }
}
`

// context: { require: true } fails the run when the source resolves to no
// stored state at all — here, the very first run.
func TestRunContextRequireFailsWithoutPriorState(t *testing.T) {
	run := runInDir(t, t.TempDir(), contextRequirePipeline)
	out, err := run()
	if err == nil {
		t.Fatalf("expected an error, got output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "resolved to no stored state") {
		t.Errorf("unexpected error: %v", err)
	}
}

const contextReadOnlyPipeline = `
pipeline Demo {
    context: {
        source: "latest"
    }

    step Mutate {
        context = 1
    }
}
`

// context is read-only: assigning to it is a fail-closed error, not a
// silent new binding.
func TestRunContextIsReadOnly(t *testing.T) {
	run := runInDir(t, t.TempDir(), contextReadOnlyPipeline)
	out, err := run()
	if err == nil {
		t.Fatalf("expected an error, got output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("unexpected error: %v", err)
	}
}

const contextBreakPipeline = `
pipeline Demo {
    checkpoint: {
        enabled: true
        strategy: "per_step"
        ttl: 7d
    }

    step One {
        var x = 1
    }

    step Two {
        break "stop here"
    }
}
`

// Two concurrent (here: sequential, but neither --resume nor --session)
// runs of the same pipeline get distinct session directories and never
// share a checkpoint file.
func TestRunConcurrentRunsGetDistinctSessionDirs(t *testing.T) {
	dir := t.TempDir()
	run := runInDir(t, dir, contextBreakPipeline)

	out1, err := run()
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	out2, err := run()
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}

	id1, id2 := sessionIDFromOutput(t, out1), sessionIDFromOutput(t, out2)
	if id1 == id2 {
		t.Fatalf("two independent runs shared a session id: %s", id1)
	}
	// A break leaves the checkpoint in place — one per session directory.
	for _, id := range []string{id1, id2} {
		p := filepath.Join(dir, ".mhl", "state", id, "Demo.json")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected a checkpoint at %s: %v", p, err)
		}
	}
}

const contextInStopWhenPipeline = `
loop pipeline Demo {
    context: {
        source: "latest"
    }
    repeat: {
        stop_when: context.session_id != ""
        max_iterations: 5
    }

    step Once {
        log("iteration")
    }
}
`

// A loop's stop_when condition can read context.* — it is threaded into
// interpreter.EvalCondition the same way `mem` is.
func TestRunContextVisibleInStopWhen(t *testing.T) {
	run := runInDir(t, t.TempDir(), contextInStopWhenPipeline)
	out, err := run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "stopped: stop_when") {
		t.Errorf("stop_when reading context.session_id should have fired after iteration 1:\n%s", out)
	}
	if strings.Count(out, "step: Once") != 1 {
		t.Errorf("expected exactly one iteration, got:\n%s", out)
	}
}

// --session pins the session directory name, on a fresh run and on --resume.
func TestRunSessionFlagPinsSession(t *testing.T) {
	dir := t.TempDir()
	run := runInDir(t, dir, contextBreakPipeline)

	out, err := run("--session", "pinned1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := sessionIDFromOutput(t, out); got != "pinned1" {
		t.Fatalf("session id = %q, want pinned1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mhl", "state", "pinned1", "Demo.json")); err != nil {
		t.Errorf("expected checkpoint under the pinned session dir: %v", err)
	}
}
