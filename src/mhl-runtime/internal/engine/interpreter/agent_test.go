package interpreter

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
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
	if _, hasLog := agentLogPath(agent); hasLog {
		t.Fatal("agentLogPath reported a log path for an agent with no log property")
	}
}
