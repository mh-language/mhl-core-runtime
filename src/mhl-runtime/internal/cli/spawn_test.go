package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// runSpawn writes src to a temp dir, runs `mhl run` from inside it, and
// returns stdout plus any error.
func runSpawn(t *testing.T, src string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "p.mh")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	var buf bytes.Buffer
	err := cli.Run(append([]string{"run", "p.mh"}, args...), &buf)
	return buf.String(), err
}

// spawn results and their captured output flush in spawn order, never
// completion order, even when a later handle finishes first.
func TestSpawnWaitAllFlushesInSpawnOrder(t *testing.T) {
	src := `
agent Slow { command: "sh" args: ["-c", "sleep 0.3; echo slow"] }
agent Fast { command: "sh" args: ["-c", "echo fast"] }

pipeline P {
    step S {
        spawn a = Slow.run(prompt: "x")
        spawn b = Fast.run(prompt: "x")
        wait a, b timeout: 5s
        log("a=${a.result}")
        log("b=${b.result}")
    }
}
`
	out, err := runSpawn(t, src)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	ai, bi := strings.Index(out, "a=slow"), strings.Index(out, "b=fast")
	if ai < 0 || bi < 0 || ai > bi {
		t.Fatalf("expected a= before b= in output:\n%s", out)
	}
}

// A pipeline-wide max_concurrency of 2 must serialize a third same-duration
// task behind the first two.
func TestSpawnMaxConcurrencyBoundsParallelism(t *testing.T) {
	src := `
agent W { command: "sh" args: ["-c", "sleep 0.3; echo done"] }

pipeline P {
    spawn: { max_concurrency: 2 }
    step S {
        spawn a = W.run(prompt: "x")
        spawn b = W.run(prompt: "x")
        spawn c = W.run(prompt: "x")
        spawn d = W.run(prompt: "x")
        wait a, b, c, d timeout: 10s
        log("all done")
    }
}
`
	start := time.Now()
	out, err := runSpawn(t, src)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "all done") {
		t.Fatalf("missing completion line:\n%s", out)
	}
	// 4 tasks / 2 slots * 0.3s = ~0.6s. Well under the 4*0.3=1.2s a serial
	// run would need and over the ~0.3s an unbounded run would need.
	if elapsed < 500*time.Millisecond {
		t.Fatalf("run finished in %s — max_concurrency not enforced", elapsed)
	}
	if elapsed > 1100*time.Millisecond {
		t.Fatalf("run took %s — more serial than max_concurrency: 2 implies", elapsed)
	}
}

// wait-all is fail-fast: the first failing spawn fails the step, naming the
// agent, and cancels the siblings.
func TestSpawnWaitAllFailFast(t *testing.T) {
	src := `
agent Ok  { command: "sh" args: ["-c", "echo ok"] }
agent Bad { command: "sh" args: ["-c", "echo boom >&2; exit 2"] }

pipeline P {
    step S {
        spawn a = Ok.run(prompt: "x")
        spawn b = Bad.run(prompt: "x")
        wait a, b timeout: 5s
        log("unreachable")
    }
}
`
	out, err := runSpawn(t, src)
	if err == nil {
		t.Fatalf("expected step failure, got success:\n%s", out)
	}
	if !strings.Contains(err.Error(), "Bad.spawn") {
		t.Fatalf("error should name the failing agent: %v", err)
	}
	if strings.Contains(out, "unreachable") {
		t.Fatalf("statement after a failed wait should not run:\n%s", out)
	}
}

// on_error: "collect" keeps the step green and records each outcome on its
// handle.
func TestSpawnWaitCollect(t *testing.T) {
	src := `
agent Ok  { command: "sh" args: ["-c", "echo good"] }
agent Bad { command: "sh" args: ["-c", "exit 2"] }

pipeline P {
    step S {
        spawn a = Ok.run(prompt: "x")
        spawn b = Bad.run(prompt: "x")
        wait a, b on_error: "collect"
        log("a.ok=${a.ok} b.ok=${b.ok} b.status=${b.status}")
    }
}
`
	out, err := runSpawn(t, src)
	if err != nil {
		t.Fatalf("collect should not fail the step: %v\n%s", err, out)
	}
	if !strings.Contains(out, "a.ok=true b.ok=false b.status=failed") {
		t.Fatalf("unexpected handle state:\n%s", out)
	}
}

// `wait N of` returns once N have succeeded and cancels the stragglers.
func TestSpawnWaitQuorum(t *testing.T) {
	src := `
agent A { command: "sh" args: ["-c", "sleep 0.1; echo a"] }
agent B { command: "sh" args: ["-c", "sleep 0.2; echo b"] }
agent C { command: "sh" args: ["-c", "sleep 30; echo c"] }

pipeline P {
    step S {
        spawn a = A.run(prompt: "x")
        spawn b = B.run(prompt: "x")
        spawn c = C.run(prompt: "x")
        wait 2 of a, b, c timeout: 10s
        log("a=${a.status} b=${b.status} c=${c.status}")
    }
}
`
	start := time.Now()
	out, err := runSpawn(t, src)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("quorum did not short-circuit the 30s straggler")
	}
	if !strings.Contains(out, "a=done b=done c=cancelled") {
		t.Fatalf("unexpected statuses:\n%s", out)
	}
}

// wait any returns on the first success and cancels the rest.
func TestSpawnWaitAny(t *testing.T) {
	src := `
agent Fast { command: "sh" args: ["-c", "echo fast"] }
agent Slow { command: "sh" args: ["-c", "sleep 30; echo slow"] }

pipeline P {
    step S {
        spawn f = Fast.run(prompt: "x")
        spawn s = Slow.run(prompt: "x")
        wait any f, s
        log("f.ok=${f.ok} s.ok=${s.ok}")
    }
}
`
	start := time.Now()
	out, err := runSpawn(t, src)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("wait any did not short-circuit the slow spawn")
	}
	if !strings.Contains(out, "f.ok=true s.ok=false") {
		t.Fatalf("unexpected outcome:\n%s", out)
	}
}

// A timeout cancels every still-running spawn and fails the step.
func TestSpawnWaitTimeout(t *testing.T) {
	src := `
agent Slow { command: "sh" args: ["-c", "sleep 30; echo slow"] }

pipeline P {
    step S {
        spawn a = Slow.run(prompt: "x")
        wait a timeout: 200ms
        log("unreachable")
    }
}
`
	start := time.Now()
	out, err := runSpawn(t, src)
	if err == nil {
		t.Fatalf("expected a timeout failure:\n%s", out)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout did not fire promptly")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error should mention the timeout: %v", err)
	}
}

// An unawaited spawn is still joined at step end, with a warning if it
// failed — it never outlives the step.
func TestSpawnUnawaitedDrainsAtStepEnd(t *testing.T) {
	src := `
agent Bad { command: "sh" args: ["-c", "exit 1"] }

pipeline P {
    step S {
        spawn a = Bad.run(prompt: "x")
        log("step body done")
    }
}
`
	out, err := runSpawn(t, src)
	if err != nil {
		t.Fatalf("an unawaited failed spawn should not fail the step: %v\n%s", err, out)
	}
	if !strings.Contains(out, "was not awaited") {
		t.Fatalf("expected a drain warning:\n%s", out)
	}
}
