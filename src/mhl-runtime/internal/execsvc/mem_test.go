package execsvc_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// Each branch of a `parallel` group works on its own copy of the run's mem,
// merged back after the barrier like a var: distinct mems written by two
// branches both survive.
func TestRunMemMergesAfterParallelGroup(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
workflow P {
    mem a = 0
    mem b = 0
    output: { a: a, b: b }
    parallel Both {
        step Left { a = a + 1 }
        step Right { b = b + 2 }
    }
}
`)
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vars["a"] != float64(1) || res.Vars["b"] != float64(2) {
		t.Errorf("Vars = %v, want a=1 b=2", res.Vars)
	}
}

// Two branches setting one mem to different values is the same conflict as
// for a var — named as a mem — and a merge block resolves it.
func TestRunMemParallelConflict(t *testing.T) {
	dir := t.TempDir()
	conflict := writeFile(t, dir, "conflict.mh", `
workflow P {
    mem n = 0
    parallel Both {
        step Left { n = 1 }
        step Right { n = 2 }
    }
}
`)
	_, err := execsvc.Run(execsvc.Request{Source: conflict, BaseDir: dir})
	if err == nil || !strings.Contains(err.Error(), `both assigned mem "n"`) {
		t.Fatalf("err = %v, want a conflict naming mem \"n\"", err)
	}

	merged := writeFile(t, dir, "merged.mh", `
workflow P {
    mem n = 0
    output: { n: n }
    parallel Both {
        step Left { n = 1 }
        step Right { n = 2 }
        merge (branches) { n = branches.Left.n + branches.Right.n }
    }
}
`)
	res, err := execsvc.Run(execsvc.Request{Source: merged, BaseDir: dir})
	if err != nil {
		t.Fatalf("run with merge: %v", err)
	}
	if res.Vars["n"] != float64(3) {
		t.Errorf("n = %v, want 3 (the merge sees each branch's mem by name)", res.Vars["n"])
	}
}

// step_start/step_end read and write the run's mem (a hook counter is the
// run's state); session_start reads the mem the run starts from, and
// session_end the one it finished with.
func TestRunMemInPipelineHooks(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
workflow P {
    mem ends = 0
    output: { ends: ends }
    on session_start(s) { log("START ends=${ends}") }
    on step_end(s) { ends = ends + 1 }
    on session_end(s) { log("END ends=${ends}") }
    step One { }
    step Two { }
}
`)
	var out bytes.Buffer
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Out: &out})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vars["ends"] != float64(2) {
		t.Errorf("ends = %v, want 2 (one per step_end)", res.Vars["ends"])
	}
	if !strings.Contains(out.String(), "START ends=0") || !strings.Contains(out.String(), "END ends=2") {
		t.Errorf("unexpected hook output:\n%s", out.String())
	}
}

// A spawned agent call reads a snapshot of the step's mem, so the step can
// keep writing it while the call runs (go test -race).
func TestRunMemSpawnReadsSnapshot(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
agent Echo {
    command: "sh"
    args: ["-c", "echo got"]
}
workflow P {
    mem n = 1
    output: { n: n }
    step Fan {
        spawn a = Echo.run(prompt: "n is ${n}")
        n = n + 1
        spawn b = Echo.run(prompt: "n is ${n}")
        n = n + 1
        wait a, b timeout 10s
    }
}
`)
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vars["n"] != float64(3) {
		t.Errorf("n = %v, want 3", res.Vars["n"])
	}
}
