package interpreter

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// TestRunAgentAttemptSharesLimiterAcrossCalls proves the *wiring*, not
// traffic.Limiter's own algorithm (already covered by
// internal/features/traffic/rate_limit_test.go): agentLimiter must hand out
// the same *traffic.Limiter on every call for a given agent, not a fresh
// (and therefore inert) one each time — the same class of bug already fixed
// for agentCache. concurrency: 1 forces the two concurrent .run()-equivalent
// calls below to serialize; if agentLimiter ever regressed to building a
// new Limiter per call, they would run fully in parallel instead.
func TestRunAgentAttemptSharesLimiterAcrossCalls(t *testing.T) {
	src := `
export agent Sleepy {
    command: "sh"
    args: ["-c", "sleep 0.2; echo done"]
    rate_limit: { concurrency: 1 }
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	agent, ok := findAgent(prog, "Sleepy")
	if !ok {
		t.Fatal("agent Sleepy not found")
	}

	// Two 0.2s calls, raced concurrently. Serialized (limiter shared, as it
	// must be) they take >=0.4s total; parallel (limiter rebuilt per call,
	// the bug) they take ~0.2s.
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = runAgentAttempt(nil, "Sleepy", agent, "hi", "")
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if elapsed < 350*time.Millisecond {
		t.Fatalf("two 200ms calls under concurrency:1 finished in %v — ran in parallel, limiter was not shared across calls", elapsed)
	}
}

// TestRunAgentAttemptAppendsStdoutToLogPath proves a `log:` property on a
// cli/* engine agent causes every call's raw subprocess stdout to be
// appended to that path — not overwritten — so a second .run() preserves
// the first call's line instead of clobbering it.
func TestRunAgentAttemptAppendsStdoutToLogPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "nested", "agent.log")
	src := `
export agent Echo {
    command: "sh"
    args: ["-c", "printf '%s\n' \"$1\"", "echo", "${prompt}"]
    log: "` + filepath.ToSlash(logPath) + `"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	agent, ok := findAgent(prog, "Echo")
	if !ok {
		t.Fatal("agent Echo not found")
	}

	if _, err := runAgentAttempt(nil, "Echo", agent, "first", ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := runAgentAttempt(nil, "Echo", agent, "second", ""); err != nil {
		t.Fatalf("second call: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	want := "first\nsecond\n"
	if string(got) != want {
		t.Fatalf("log content = %q, want %q", string(got), want)
	}
}

// TestAgentLogPathInterpolatesSpans proves a `log:` path is interpolated for
// "${...}" spans against the calling ctx exactly like a prompt string, so
// `log: "logs/run.${run_id}.log"` resolves to a per-run file rather than one
// shared path every concurrent run appends into.
func TestAgentLogPathInterpolatesSpans(t *testing.T) {
	src := `
export agent Echo {
    command: "sh"
    args: ["-c", "echo hi"]
    log: "logs/run.${run_id}.log"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	agent, ok := findAgent(prog, "Echo")
	if !ok {
		t.Fatal("agent Echo not found")
	}

	ctx := &evalCtx{prog: prog, env: Env{"run_id": "abc123"}, out: io.Discard}
	got, hasLog, err := agentLogPath(ctx, agent)
	if err != nil {
		t.Fatalf("agentLogPath: %v", err)
	}
	if !hasLog || got != "logs/run.abc123.log" {
		t.Fatalf("agentLogPath = %q (hasLog=%v), want %q", got, hasLog, "logs/run.abc123.log")
	}
}

// TestRunAgentAttemptWithoutLogPropertyWritesNoLog proves the log write is
// opt-in: an agent without a `log:` property must not create any file as a
// side effect of running.
func TestRunAgentAttemptWithoutLogPropertyWritesNoLog(t *testing.T) {
	src := `
export agent Echo {
    command: "sh"
    args: ["-c", "printf '%s\n' \"$1\"", "echo", "${prompt}"]
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	agent, ok := findAgent(prog, "Echo")
	if !ok {
		t.Fatal("agent Echo not found")
	}

	if _, err := runAgentAttempt(nil, "Echo", agent, "hi", ""); err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, hasLog, err := agentLogPath(nil, agent); err != nil || hasLog {
		t.Fatalf("agentLogPath reported a log path for an agent with no log property (hasLog=%v err=%v)", hasLog, err)
	}
}

// TestRunAgentAttemptDiskCacheKeyIncludesInvocationIdentity is the V2
// regression from the technical assessment: two agents that share the
// on-disk cache directory (`storage: "disk"`) but differ only in their
// `command`/`args` must not collide on one cache entry. Before the fix the
// key folded only engine+prompt+schema, so B's first call returned A's
// cached response.
func TestRunAgentAttemptDiskCacheKeyIncludesInvocationIdentity(t *testing.T) {
	t.Chdir(t.TempDir()) // isolate the relative .mhl-cache directory

	src := `
export agent A {
    command: "echo"
    args: ["response-A"]
    cache: { ttl: 1h, storage: "disk" }
}
export agent B {
    command: "echo"
    args: ["response-B"]
    cache: { ttl: 1h, storage: "disk" }
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	agentA, ok := findAgent(prog, "A")
	if !ok {
		t.Fatal("agent A not found")
	}
	agentB, ok := findAgent(prog, "B")
	if !ok {
		t.Fatal("agent B not found")
	}

	gotA, err := runAgentAttempt(nil, "A", agentA, "same-prompt", "")
	if err != nil {
		t.Fatalf("A.run: %v", err)
	}
	if gotA != "response-A same-prompt" {
		t.Fatalf("A returned %q, want %q", gotA, "response-A same-prompt")
	}

	gotB, err := runAgentAttempt(nil, "B", agentB, "same-prompt", "")
	if err != nil {
		t.Fatalf("B.run: %v", err)
	}
	if gotB != "response-B same-prompt" {
		t.Fatalf("B returned %q (want %q) — cache key collided with agent A", gotB, "response-B same-prompt")
	}
}
