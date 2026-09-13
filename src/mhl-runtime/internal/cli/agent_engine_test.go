package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestRunOllamaAgentHappyPath exercises an `engine: "ollama/..."` agent
// end-to-end through `mhl run`, using a local httptest.Server standing in
// for a real Ollama instance (no real Ollama or network access needed).
func TestRunOllamaAgentHappyPath(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "42"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Local {
    engine: "ollama/qwen2.5-coder"
    endpoint: "` + srv.URL + `"
    temperature: 0.2
    trace: true
}

pipeline P {
    step S {
        var response = Local.run(prompt: "what is the answer?")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "agent Local response:\n42\n") {
		t.Errorf("unexpected output: %s", buf.String())
	}
	if gotBody["model"] != "qwen2.5-coder" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["prompt"] != "what is the answer?" {
		t.Errorf("prompt = %v", gotBody["prompt"])
	}
}

func TestRunOllamaAgentMissingEndpoint(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent Local {
    engine: "ollama/qwen2.5-coder"
}

pipeline P {
    step S {
        var response = Local.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for an ollama agent with no endpoint")
	}
	if !strings.Contains(err.Error(), `agent "Local" has no endpoint`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunOllamaAgentBadTemperature(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent Local {
    engine: "ollama/qwen2.5-coder"
    endpoint: "http://localhost:11434"
    temperature: "warm"
}

pipeline P {
    step S {
        var response = Local.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a non-numeric temperature")
	}
	if !strings.Contains(err.Error(), `agent "Local" temperature must be a number`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunUnsupportedEngine(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent Cloud {
    engine: "anthropic/claude-3-5-sonnet"
    api_key: "sk-fake"
}

pipeline P {
    step S {
        var response = Cloud.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for an unsupported (cloud API) engine")
	}
	if !strings.Contains(err.Error(), `agent "Cloud": engine "anthropic/claude-3-5-sonnet" is not supported yet`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunCLIAgentPromptPlacedByPlaceholder confirms an args entry of exactly
// "${prompt}" marks where the resolved prompt is inserted — not necessarily
// the last argument — so a CLI (e.g. `claude -p <prompt> --agent flow`)
// that requires its prompt immediately after a specific flag can declare
// that position explicitly.
func TestRunCLIAgentPromptPlacedByPlaceholder(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent LocalEcho {
    command: "echo"
    args: ["-p", "${prompt}", "--agent", "flow", "--verbose"]
    trace: true
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "hi there")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "-p hi there --agent flow --verbose") {
		t.Errorf("expected the prompt placed right after -p, got: %s", buf.String())
	}
}

// TestRunCLIAgentPromptAppendedWithoutPlaceholder is the regression case:
// an agent with no "${prompt}" placeholder in args keeps the original
// behavior of appending the prompt as the final argument.
func TestRunCLIAgentPromptAppendedWithoutPlaceholder(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent LocalEcho {
    command: "echo"
    args: ["-p", "--dangerously-skip-permissions"]
    trace: true
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "hi there")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "-p --dangerously-skip-permissions hi there") {
		t.Errorf("expected the prompt appended last, got: %s", buf.String())
	}
}

// TestRunCLIAgentCommandFromEnv is MHL-Melhorias.md #1: `command`/`args` can
// now be any expression, not just a string literal, so `command: env("VAR")`
// lets one agent declaration switch backend by environment instead of
// needing a whole duplicated .mh file per environment. args mixes an
// env(...) element with the pre-existing "${prompt}" placeholder token, to
// prove the two coexist: env(...) is resolved through the real evaluator,
// while "${prompt}" is still substituted by injectPromptArg afterward
// rather than interpolated as a variable reference.
func TestRunCLIAgentCommandFromEnv(t *testing.T) {
	// auth's secret registry is process-global and never reset between
	// tests in this package (see other env(...) tests in this file); any
	// env(...) resolved through a declaration property is remembered
	// unconditionally (RememberRef, regardless of length), so a short,
	// ordinary-looking value like "echo" would get redacted out of every
	// later test's output in this same binary that happens to print that
	// substring. Distinctive values sidestep that instead of resetting
	// global state no other test here resets either.
	echoPath, err := exec.LookPath("echo")
	if err != nil {
		t.Skipf("echo not found on PATH: %v", err)
	}
	t.Setenv("MHL_TEST_ECHO_CMD_1", echoPath)
	t.Setenv("MHL_TEST_ECHO_FLAG_1", "--mhl-test-marker-flag-1")
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent LocalEcho {
    command: env("MHL_TEST_ECHO_CMD_1")
    args: [env("MHL_TEST_ECHO_FLAG_1"), "-p", "${prompt}"]
    trace: true
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "hi there")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	// The env(...)-sourced flag is registered as a credential the same way
	// any extension endpoint's env(...) is (resolveCommandLikeExpr mirrors
	// resolveExtensionDeclaration), so trace output redacts it —
	// "[REDACTED]" in its place, right before "-p hi there", is proof both
	// that env(...) actually ran (echo's real argv did receive it; only the
	// trace print is redacted) and that "${prompt}" still landed via its
	// own placeholder substitution, not variable interpolation.
	if !strings.Contains(buf.String(), "[REDACTED] -p hi there") {
		t.Errorf("expected env(...)-resolved command/args (redacted in trace) with the prompt placed by its placeholder, got: %s", buf.String())
	}
}

// TestRunCLIAgentCommandFromEnvMissingFailsClosed confirms a `command:
// env("VAR")` referencing an unset variable fails the run with a clear
// error — the same fail-closed guarantee extension declarations already
// have (auth.Resolve) — rather than silently invoking an empty command.
func TestRunCLIAgentCommandFromEnvMissingFailsClosed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent LocalEcho {
    command: env("MHL_TEST_ECHO_CMD_DOES_NOT_EXIST")
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "hi there")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatalf("expected a fail-closed error for an unset env var, got success: %s", buf.String())
	}
}

// TestRunCLIAgentClaudeStyleCommandShape mirrors a Python helper that builds
// a `claude` CLI invocation:
//
//	def _claude_command(flow: str, model: str | None = None) -> list[str]:
//	    return [
//	        "claude", *_model_args("claude", model),
//	        "-p", _claude_prompt(flow),
//	        "--agent", flow,
//	        "--permission-mode", "bypassPermissions",
//	        "--output-format", "stream-json",
//	        "--verbose",
//	    ]
//
// declared as an MHL agent (command swapped for echo so the test can assert
// on the exact argv instead of calling the real `claude` binary), confirming
// the prompt lands right after "-p" and every other flag/value survives in
// order.
func TestRunCLIAgentClaudeStyleCommandShape(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent ClaudeCLI {
    command: "echo"
    args: [
        "-p", "${prompt}",
        "--agent", "security-audit",
        "--permission-mode", "bypassPermissions",
        "--output-format", "stream-json",
        "--verbose"
    ]
    trace: true
}

pipeline P {
    step S {
        var response = ClaudeCLI.run(prompt: "Revise a PR #42")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "-p Revise a PR #42 --agent security-audit --permission-mode bypassPermissions --output-format stream-json --verbose"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("expected argv %q, got: %s", want, buf.String())
	}
}

// TestRunCLIAgentWithExplicitEngine is a regression test: an agent using the
// spec's exact "LocalClaudeCLI" shape (engine: "cli/claude-code" +
// command/args), with the real `claude` binary swapped for `echo`, must keep
// running exactly as it did before engine dispatch existed.
func TestRunCLIAgentWithExplicitEngine(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent LocalEcho {
    engine: "cli/claude-code"
    command: "echo"
    args: ["--non-interactive"]
    trace: true
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "agent LocalEcho response:") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestRunCLIAgentLogsStdoutToLogPath proves the `log:` property on a cli/*
// engine agent appends every call's raw subprocess stdout to that file
// through the same `mhl run` path a real user drives (cli.Run) — not just
// runAgentAttempt in isolation. Two separate `mhl run` invocations each
// call the agent once; both lines must survive in the log, in order,
// proving the second run appends rather than overwrites the first.
func TestRunCLIAgentLogsStdoutToLogPath(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "echo.log")
	main := filepath.Join(dir, "main.mh")
	source := `
agent LocalEcho {
    command: "sh"
    args: ["-c", "printf '%s\n' \"$1\"", "echo", "${prompt}"]
    log: "` + filepath.ToSlash(logPath) + `"
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "` + `PROMPT` + `")
    }
}
`
	writeMain := func(prompt string) {
		t.Helper()
		if err := os.WriteFile(main, []byte(strings.Replace(source, "PROMPT", prompt, 1)), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	writeMain("first")
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("first run: %v", err)
	}

	writeMain("second")
	buf.Reset()
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("second run: %v", err)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if want := "first\nsecond\n"; string(got) != want {
		t.Errorf("log content = %q, want %q", string(got), want)
	}
}

// TestRunCLIAgentReturnsRawStreamVerbatimRegardlessOfEngine is a regression
// test: `.run()` must never parse or reshape a cli/* agent's stdout on the
// runtime's own initiative — not even for engines matching a real CLI's
// name like "cli/codex" or "cli/claude-code", whose streaming NDJSON output
// format is that CLI's contract, not mhl's. Baking a per-engine "which
// field of which event is the real answer" rule into the Go runtime would
// mean a new mhl release is needed every time Claude or Codex changes that
// shape; TestToolJSONParseLinesExtractsFinalCodexAgentMessage (tool_test.go)
// is the intended replacement — that contract lives in ordinary .mh code
// (json.parse_lines + filter + indexing), editable without recompiling
// anything.
func TestRunCLIAgentReturnsRawStreamVerbatimRegardlessOfEngine(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	fixture, err := filepath.Abs("testdata/codex_json_stream_response.ndjson")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	src := `
agent CodexCLI {
    engine: "cli/codex"
    command: "sh"
    args: ["-c", "cat '` + filepath.ToSlash(fixture) + `'", "codex", "${prompt}"]
}

pipeline P {
    step S {
        var response = CodexCLI.run(prompt: "setup")
        log("lines=${response.split(\"\n\").size()}")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fixtureContent, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	wantLines := len(strings.Split(strings.TrimRight(string(fixtureContent), "\n"), "\n"))

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := fmt.Sprintf("lines=%d", wantLines)
	if !strings.Contains(buf.String(), want) {
		t.Errorf("expected %q (the untouched multi-line stream) in output, got: %s", want, buf.String())
	}
}
