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
			_, errs[i] = runAgentAttempt(nil, "Sleepy", agent, "hi", "", 0)
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

	if _, err := runAgentAttempt(nil, "Echo", agent, "first", "", 0); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := runAgentAttempt(nil, "Echo", agent, "second", "", 0); err != nil {
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

	if _, err := runAgentAttempt(nil, "Echo", agent, "hi", "", 0); err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, hasLog, err := agentLogPath(nil, agent); err != nil || hasLog {
		t.Fatalf("agentLogPath reported a log path for an agent with no log property (hasLog=%v err=%v)", hasLog, err)
	}
}

// TestRunAgentAttemptWritesPromptToStdin proves a `stdin: "${prompt}"`
// property actually delivers promptText to the subprocess's standard input
// — this is the mechanism MHL-Melhorias.md's Windows argv-limit finding
// asks for: a large prompt routed here never touches argv, so it can't run
// into CreateProcess's ~32,767-character command-line cap.
func TestRunAgentAttemptWritesPromptToStdin(t *testing.T) {
	src := `
export agent Echo {
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt}"
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

	got, err := runAgentAttempt(nil, "Echo", agent, "hello via stdin", "", 0)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got != "hello via stdin" {
		t.Fatalf("response = %q, want the prompt echoed back via stdin", got)
	}
}

// TestRunAgentAttemptStdinAndLogAreIndependent proves `stdin:` and `log:`
// don't interfere with each other: they're separate tools.Cmd fields wired
// to separate pipes (command.Stdin vs command.Stdout), so a call still
// returns the subprocess's real stdout — not an echo of what was piped in,
// not something corrupted by also having stdin set — and the log file still
// accumulates every call's raw stdout exactly as it did before `stdin:`
// existed (TestRunAgentAttemptAppendsStdoutToLogPath's same assertion,
// combined here with an agent whose prompt travels through stdin).
func TestRunAgentAttemptStdinAndLogAreIndependent(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "agent.log")
	src := `
export agent Echo {
    command: "sh"
    args: ["-c", "cat | tr a-z A-Z; echo"]
    stdin: "${prompt}"
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

	got1, err := runAgentAttempt(nil, "Echo", agent, "first", "", 0)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got1 != "FIRST" {
		t.Fatalf("first response = %q, want %q (stdin delivered, real subprocess output returned)", got1, "FIRST")
	}
	got2, err := runAgentAttempt(nil, "Echo", agent, "second", "", 0)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got2 != "SECOND" {
		t.Fatalf("second response = %q, want %q", got2, "SECOND")
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if want := "FIRST\nSECOND\n"; string(logged) != want {
		t.Fatalf("log content = %q, want %q — log capture must be unaffected by stdin being set", string(logged), want)
	}
}

// TestRunAgentAttemptStdinPromptIsNotAlsoAppendedToArgs proves that once
// `stdin:` already claims "${prompt}", injectPromptArg's normal
// append-at-end fallback (for an agent with no placeholder anywhere) does
// not also put the full prompt into argv — routing a large prompt through
// stdin would be pointless if it silently still went into args: too.
func TestRunAgentAttemptStdinPromptIsNotAlsoAppendedToArgs(t *testing.T) {
	// sh -c SCRIPT [$0 [$1 ...]]: with no extra argv element appended, $0
	// stays at sh's own default; with one appended (the old prompt-append
	// fallback), that appended text becomes $0. Reading $0 back this way
	// exposes whether promptText actually reached argv, not just how many
	// elements got appended.
	src := `
export agent ViaStdin {
    command: "sh"
    args: ["-c", "printf '%s' \"$0\""]
    stdin: "${prompt}"
}
export agent ViaArgs {
    command: "sh"
    args: ["-c", "printf '%s' \"$0\""]
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	viaStdin, ok := findAgent(prog, "ViaStdin")
	if !ok {
		t.Fatal("agent ViaStdin not found")
	}
	viaArgs, ok := findAgent(prog, "ViaArgs")
	if !ok {
		t.Fatal("agent ViaArgs not found")
	}

	gotStdin, err := runAgentAttempt(nil, "ViaStdin", viaStdin, "a prompt", "", 0)
	if err != nil {
		t.Fatalf("ViaStdin call: %v", err)
	}
	if gotStdin == "a prompt" {
		t.Fatalf("ViaStdin's $0 = %q — prompt reached argv even though stdin: \"${prompt}\" already claimed it", gotStdin)
	}

	gotArgs, err := runAgentAttempt(nil, "ViaArgs", viaArgs, "a prompt", "", 0)
	if err != nil {
		t.Fatalf("ViaArgs call: %v", err)
	}
	if gotArgs != "a prompt" {
		t.Fatalf("ViaArgs's $0 = %q, want %q (unchanged default: prompt appended to args when no stdin claims it)", gotArgs, "a prompt")
	}
}

// TestRunAgentAttemptStdinSchemaPlaceholderRequiresSchema proves
// `stdin: "${schema}"` fails closed — same as args' "${schema}" element —
// rather than leaking the literal placeholder text to the subprocess when a
// call supplies no schema.
func TestRunAgentAttemptStdinSchemaPlaceholderRequiresSchema(t *testing.T) {
	src := `
export agent Echo {
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${schema}"
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

	_, err = runAgentAttempt(nil, "Echo", agent, "hi", "", 0)
	if err == nil {
		t.Fatal("expected an error when stdin declares \"${schema}\" but no schema was supplied")
	}
}

// TestRunAgentAttemptDiskCacheKeyIncludesStdin proves two agents that share
// the on-disk cache directory but differ only in `stdin:` (identical
// command/args) don't collide on one cache entry — the same class of bug
// TestRunAgentAttemptDiskCacheKeyIncludesInvocationIdentity already covers
// for command/args.
func TestRunAgentAttemptDiskCacheKeyIncludesStdin(t *testing.T) {
	t.Chdir(t.TempDir())

	src := `
export agent A {
    command: "sh"
    args: ["-c", "cat"]
    stdin: "stdin-A"
    cache: { ttl: 1h, storage: "disk" }
}
export agent B {
    command: "sh"
    args: ["-c", "cat"]
    stdin: "stdin-B"
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

	gotA, err := runAgentAttempt(nil, "A", agentA, "same-prompt", "", 0)
	if err != nil {
		t.Fatalf("A.run: %v", err)
	}
	if gotA != "stdin-A" {
		t.Fatalf("A returned %q, want %q", gotA, "stdin-A")
	}

	gotB, err := runAgentAttempt(nil, "B", agentB, "same-prompt", "", 0)
	if err != nil {
		t.Fatalf("B.run: %v", err)
	}
	if gotB != "stdin-B" {
		t.Fatalf("B returned %q (want %q) — cache key collided with agent A", gotB, "stdin-B")
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

	gotA, err := runAgentAttempt(nil, "A", agentA, "same-prompt", "", 0)
	if err != nil {
		t.Fatalf("A.run: %v", err)
	}
	if gotA != "response-A same-prompt" {
		t.Fatalf("A returned %q, want %q", gotA, "response-A same-prompt")
	}

	gotB, err := runAgentAttempt(nil, "B", agentB, "same-prompt", "", 0)
	if err != nil {
		t.Fatalf("B.run: %v", err)
	}
	if gotB != "response-B same-prompt" {
		t.Fatalf("B returned %q (want %q) — cache key collided with agent A", gotB, "response-B same-prompt")
	}
}
