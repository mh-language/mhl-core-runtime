package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestFindingString(t *testing.T) {
	f := lint.Finding{File: "main.mh", Line: 12, Message: `agent "Ghost" not found`}
	want := `main.mh:12: error: agent "Ghost" not found`
	if got := f.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestCheckAgentNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var response = Ghost.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
	if findings[0].Line != 4 {
		t.Errorf("expected line 4, got %d", findings[0].Line)
	}
}

func TestCheckAgentArgsNotArray(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    args: "not-an-array"
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" args must be an array of strings`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
	if findings[0].Line != 2 {
		t.Errorf("expected line 2 (agent decl), got %d", findings[0].Line)
	}
}

func TestCheckAgentMissingCommand(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    args: ["--flag"]
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" has no command`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckRunMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
}

pipeline P {
    step S {
        var response = Reviewer.run(x: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `Reviewer.run requires a non-empty prompt`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckOllamaAgentMissingEndpoint(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Local {
    engine: "ollama/qwen2.5-coder"
}

pipeline P {
    step S {
        var response = Local.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Local" has no endpoint`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckOllamaAgentBadTemperature(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
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
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Local" temperature must be a number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckOllamaAgentValid confirms a correctly configured ollama agent
// produces zero findings — and, critically, does so without lint ever
// making a network call (no httptest.Server anywhere in this test): lint is
// purely static for the ollama path just like it is for the cli path.
func TestCheckOllamaAgentValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Local {
    engine: "ollama/qwen2.5-coder"
    endpoint: "http://localhost:11434"
}

pipeline P {
    step S {
        var response = Local.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckUnsupportedEngine(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Cloud {
    engine: "anthropic/claude-3-5-sonnet"
}

pipeline P {
    step S {
        var response = Cloud.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Cloud": engine "anthropic/claude-3-5-sonnet" is not supported yet`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckKVMemoryValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("attempt", "1")
        session_mem.get("attempt", "0")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckKVMemorySetWrongArgCount(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("attempt")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `set requires (key, value)`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckKVMemoryUnsupportedStore(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "redis"
}

pipeline P {
    step S {
        session_mem.set("k", "v")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `store "redis" is not supported yet`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckJSONMemoryValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "json"
    path: "./.mhl/session.json"
}

pipeline P {
    step S {
        session_mem.set("attempt", "1")
        session_mem.get("attempt", "0")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mhl", "session.json")); err == nil {
		t.Errorf("lint must not write to the json memory file")
	}
}

func TestCheckJSONMemoryMissingPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "json"
}

pipeline P {
    step S {
        session_mem.set("k", "v")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `memory "session_mem" has no path`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckJSONMemorySetWrongArgCount(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "json"
    path: "./.mhl/session.json"
}

pipeline P {
    step S {
        session_mem.set("attempt")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `set requires (key, value)`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckJSONLMemoryValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "jsonl"
    path: "./logs/audit.jsonl"
}

pipeline P {
    step S {
        audit_log.append("entry")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs", "audit.jsonl")); err == nil {
		t.Errorf("lint must not write to the jsonl memory file")
	}
}

func TestCheckJSONLMemoryMissingPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "jsonl"
}

pipeline P {
    step S {
        audit_log.append("entry")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `memory "audit_log" has no path`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckJSONLMemoryWrongMethod(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "jsonl"
    path: "./logs/audit.jsonl"
}

pipeline P {
    step S {
        audit_log.set("k", "v")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `jsonl memory has no method "set"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckJSONMemoryArrayObjectValueValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "json"
    path: "./.mhl/session.json"
}

pipeline P {
    step S {
        session_mem.set("tags", ["a", "b"])
        session_mem.set("cfg", {retries: 3})
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckAppendLogMemoryRejectsStructuredValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "append_log"
    path: "./logs/audit.log"
}

pipeline P {
    step S {
        audit_log.append(["a", "b"])
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `append_log entries must be plain text`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckJSONLMemoryAcceptsObjectValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "jsonl"
    path: "./logs/audit.jsonl"
}

pipeline P {
    step S {
        audit_log.append({event: "started"})
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckMemoryValueMaxDepthAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	nested := strings.Repeat("[", 10) + "1" + strings.Repeat("]", 10)
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("deep", `+nested+`)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckMemoryValueExceedsMaxDepth(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	nested := strings.Repeat("[", 11) + "1" + strings.Repeat("]", 11)
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("deep", `+nested+`)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "value nesting exceeds the maximum depth of 10") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAppendLogMemoryValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "append_log"
    path: "./logs/audit.log"
}

pipeline P {
    step S {
        audit_log.append("entry")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
	// lint must never have side effects: confirm it did not actually create
	// the log file it just validated.
	if _, err := os.Stat(filepath.Join(dir, "logs", "audit.log")); err == nil {
		t.Errorf("lint must not write to the append_log file")
	}
}

func TestCheckAppendLogMemoryMissingPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "append_log"
}

pipeline P {
    step S {
        audit_log.append("entry")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `memory "audit_log" has no path`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckMemoryNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        ghost_mem.set("k", "v")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `memory "ghost_mem" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// An `extension store S { ... }` name shares its method names (get/set/append)
// with `memory`; a call like `var v = S.get("k")` must not be flagged as
// `memory "S" not found` — the extension declares the target.
func TestExtensionMethodCallNotFlaggedAsMemory(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
extension store S {
    dir: "/tmp/x"
}

pipeline P {
    step seed {
        S.put("n", 0)
    }
    step bump {
        var v = S.get("n")
        S.put("n", v + 1)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckMemoryUnsupportedType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory project_rag {
    type: "vector"
    provider: "chroma"
}

pipeline P {
    step S {
        project_rag.get("k")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `type "vector" is not supported yet`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckMemoryWrongMethodForType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory audit_log {
    type: "append_log"
    path: "audit.log"
}

pipeline P {
    step S {
        audit_log.set("k", "v")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `append_log memory has no method "set"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCatchesProblemInsideIfBlock locks in the current scope: the runtime
// (internal/engine/interpreter.execBlock) executes if/while/try for real now, so a call to
// an undeclared agent nested inside one of those blocks does fail `mhl run`
// — lint must recurse into them too and report it, not stay silent the way
// it used to when those blocks were still dead syntax.
func TestCatchesProblemInsideIfBlock(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var flag = true
        if (flag) {
            var response = Ghost.run(prompt: "hi")
        }
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestNoFalsePositiveInsideUnreachedElseBlock exercises the honest
// limitation documented on collectVarNames/checkExprCall: lint checks every
// branch structurally, regardless of whether it's the one that would
// actually run — so a bad call in an `else` branch is still reported even
// though `flag` is always true here and that branch never executes at
// runtime. This is a deliberate false positive over a false negative.
func TestNoFalsePositiveInsideUnreachedElseBlock(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var flag = true
        if (flag) {
            log("taken")
        } else {
            var response = Ghost.run(prompt: "hi")
        }
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (the unreached else branch is still checked), got %d: %+v", len(findings), findings)
	}
}

func TestCheckCatchesProblemInsideWhileBlock(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var i = 0
        while (i < 1) {
            var response = Ghost.run(prompt: "hi")
            i = i + 1
        }
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckCatchesProblemInsideTryCatchFinally(t *testing.T) {
	for _, block := range []string{"try", "catch", "finally"} {
		t.Run(block, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.mh")
			bad := `var response = Ghost.run(prompt: "hi")`
			bodies := map[string]string{"try": "", "catch": "", "finally": ""}
			bodies[block] = bad
			write(t, main, `
pipeline P {
    step S {
        try {
            `+bodies["try"]+`
        } catch (e) {
            `+bodies["catch"]+`
        } finally {
            `+bodies["finally"]+`
        }
    }
}
`)
			findings := lint.File(main)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
			}
			if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
				t.Errorf("unexpected message: %q", findings[0].Message)
			}
		})
	}
}

func TestCheckAssignToUndeclaredVariable(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        y = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `undefined variable "y"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAssignToDeclaredVariableIsValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var attempt = 0
        attempt = attempt + 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckAssignToPipelineVarIsValid is the static-lint mirror of
// interpreter.execAssign's pipelineEnv fallback: a plain assignment to a
// pipeline-level `var` (declared outside any step) must not be flagged
// "undefined variable" just because collectVarNames only ever looked at
// the step's own body.
// TestUseResolvesTransitivelyThroughIntermediateModule mirrors
// internal/cli's TestRunTransitiveUseResolvesThroughIntermediateModule: a
// pipeline `use`s a tool from one file, and that tool's own method body
// depends on a memory declared in a third file, reached only through the
// tool's own separate `use`. checkToolBlocks walks into a block-bodied
// tool method's statements the same way checkAgentCalls walks a step's, so
// `Store.get(...)` inside Counter.pending()'s block gets checkExprCall's
// findMemory(prog, "Store") check too — before mergeImports resolved
// transitively, that failed with "memory \"Store\" not found", the exact
// static-analysis mirror of the runtime error this fix closes.
func TestUseResolvesTransitivelyThroughIntermediateModule(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "memory.mh"), `
export memory Store {
    type: "kv"
    store: "memory"
}
`)
	write(t, filepath.Join(dir, "tool.mh"), `
use {Store} from "memory.mh"

export tool Counter {
    pending() -> {
        return Store.get("n", 0)
    }
}
`)
	main := filepath.Join(dir, "pipeline.mh")
	write(t, main, `
use {Counter} from "tool.mh"

pipeline P {
    step S {
        log("pending=${Counter.pending()}")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckAssignToPipelineVarIsValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    var pending = ["a", "b"]

    step S {
        pending = pending[1..]
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckAssignToPipelineVarFromADifferentPipelineIsUndefined confirms
// collectPipelineVarNames is scoped per pipeline, not merged across every
// pipeline in the file — a step in P must not see Q's pipeline var.
func TestCheckAssignToPipelineVarFromADifferentPipelineIsUndefined(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline Q {
    var shared = 1
}

pipeline P {
    step S {
        shared = 2
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `undefined variable "shared"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckAssignToDeclaredVariableInEarlierIfBranchIsValid documents the
// approximation collectVarNames makes: it's structural, not flow-sensitive,
// so a `var` declared inside one `if` branch is treated as valid for an
// `assign` anywhere else in the step — matching the runtime's own flat,
// step-scoped Env (internal/engine/interpreter.Env), which would only actually fail this
// at run time if the branch that declares it is never taken.
func TestCheckAssignToDeclaredVariableInEarlierIfBranchIsValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var flag = true
        if (flag) {
            var attempt = 0
        }
        attempt = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckAssignToMemberTargetIsInvalid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var obj = {a: 1}
        obj.a = 2
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "assignment target must be a plain variable") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAssignToIndexTargetIsValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var arr = [1, 2, 3]
        arr[0] = 99
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckAssignToChainedIndexTargetIsValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var matrix = [[1, 2], [3, 4]]
        matrix[0][1] = 99
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckAssignToUndeclaredIndexTargetIsInvalid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        arr[0] = 99
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `undefined variable "arr"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAssignToMemberThenIndexTargetIsInvalid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var obj = {list: [1, 2, 3]}
        obj.list[0] = 99
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "assignment target must be a plain variable or an array index") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckMemoryOpWithVariableArgumentIsNotFlagged documents the necessary
// relaxation: the runtime now accepts variables as memory-op arguments
// (evalPositionalValues), so lint can no longer demand every argument be a
// literal — only argument *counts*, and literal arguments' types, are
// still checked. See checkMemoryOp's doc comment.
func TestCheckMemoryOpWithVariableArgumentIsNotFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        var key = "attempt"
        var value = 1
        session_mem.set(key, value)
        session_mem.get(key)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckMemoryOpKeyLiteralWrongTypeStillFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set(1, "value")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "key must be a string") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckMemoryOpArgCountStillCheckedWithVariables confirms arity is
// checked independently of whether arguments are literals or variables.
func TestCheckMemoryOpArgCountStillCheckedWithVariables(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        var key = "attempt"
        session_mem.set(key)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "set requires (key, value)") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckToolCallValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    add(a, b) -> a + b
}

pipeline P {
    step S {
        execution.add(1, 2)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckToolNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        ghost.add(1, 2)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (an undeclared target that isn't a memory method name isn't checked), got %d: %+v", len(findings), findings)
	}
}

func TestCheckToolMethodNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    add(a, b) -> a + b
}

pipeline P {
    step S {
        execution.missing()
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `tool "execution" has no method "missing"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckToolCallWrongArgCount(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    add(a, b) -> a + b
}

pipeline P {
    step S {
        execution.add(1)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "requires 2 argument(s), got 1") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckNativeNamespaceCallNeverFlagged confirms cmd/git/fs/http/time calls
// never produce a finding — lint never executes anything (no subprocess, no
// filesystem, no network), so there's nothing it can statically verify
// beyond the shape already being a well-formed call.
func TestCheckNativeNamespaceCallNeverFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        cmd.exec("does-not-exist-anywhere")
        fs.read("/no/such/file")
        git.diff(target: "nonsense")
        http.post(url: "not a real url")
        time.now()
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckToolBlockBodyCatchesBadAgentCall(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool T {
    f(x) -> {
        var y = Ghost.run(prompt: "hi")
        return y
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckToolBlockBodyCatchesBadReturnValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool T {
    f() -> {
        return session_mem.get("k")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `memory "session_mem" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckToolBlockBodyValidIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool T {
    add(a, b) -> {
        var sum = a + b
        return sum
    }
}

pipeline P {
    step S {
        T.add(1, 2)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckReturnInStepCatchesBadCall(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        return Ghost.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckBareReturnIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        return
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckForInCatchesBadCallInBody(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        for (var item in [1, 2]) {
            var response = Ghost.run(prompt: "hi")
        }
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckForInVariableUsableAfterLoop(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        for (var item in [1, 2]) {
            log(item)
        }
        item = 3
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckForInValidIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        for (var item in [1, 2, 3]) {
            log(item)
        }
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckJSONParseCallResolvesWithoutFinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        var parsed = json.parse("{\"a\":1}")
        log(parsed.a)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckDynamicPromptResolvesWithoutFinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt Greeting(name: string) {
    "Hello, ${name}!"
}

agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting(name: "World"))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckDynamicPromptNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Ghost(name: "World"))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `prompt "Ghost" not found`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckDynamicPromptNestedResolvesWithoutFinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt Role() {
    "a security expert"
}

prompt Greeting(role: prompt, name: string) {
    "You are ${role}. Hello, ${name}!"
}

agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting(role: Role(), name: "World"))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckDynamicPromptNestedReusedTwiceResolvesWithoutFinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt Role(title: string) {
    "a ${title} expert"
}

prompt Comparison(role: prompt, other_role: prompt) {
    "${role} vs ${other_role}"
}

agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Comparison(
            role: Role(title: "security"),
            other_role: Role(title: "performance")
        ))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckDynamicPromptMissingParam(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt Greeting(name: string) {
    "Hello, ${name}!"
}

agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting())
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `missing value for parameter "name"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckDynamicPromptRuntimeArgumentDoesNotFalsePositive is a regression
// test: internal/engine/interpreter.renderPromptCallDynamic now accepts any
// expression for a prompt template's own arguments (a variable,
// `feature.title`, ...), not just a string literal or a nested prompt
// reference — lint has no way to evaluate `feature.title` and prove
// anything about it, so it must accept the shape (the argument name still
// matches a declared parameter) without claiming it "must be a string
// literal or a prompt reference", which used to be a false positive on
// perfectly valid code.
func TestCheckDynamicPromptRuntimeArgumentDoesNotFalsePositive(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt FixPrompt(title: string) {
    "Corrija a feature: ${title}"
}

agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var feature = {"title": "checkout"}
        var response = Echo.run(prompt: FixPrompt(title: feature.title))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckDynamicPromptRuntimeArgumentUnexpectedNameStillErrors proves the
// previous test's leniency doesn't swallow a genuine mistake: an argument
// name that doesn't match any of the prompt's declared parameters is still
// flagged, even though its value is a runtime expression lint can't
// evaluate — the check is on the argument's *name*, which is always
// statically known, not on its unevaluable value.
func TestCheckDynamicPromptRuntimeArgumentUnexpectedNameStillErrors(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt FixPrompt(title: string) {
    "Corrija a feature: ${title}"
}

agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var feature = {"title": "checkout"}
        var response = Echo.run(prompt: FixPrompt(subtitle: feature.title))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `unexpected argument "subtitle"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckPromptArgumentAsVariableDoesNotFalsePositive is a regression
// test for the other half of the same change: `prompt:` itself now accepts
// any string-valued expression at runtime (resolvePromptArgument,
// internal/engine/interpreter/prompt_ops.go), not just a literal or a bare
// `Name(...)` reference — a variable holding a previously-rendered prompt
// must not be flagged as "requires a non-empty prompt" just because lint
// can't prove what it holds.
func TestCheckPromptArgumentAsVariableDoesNotFalsePositive(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Echo {
    command: "echo"
}

pipeline P {
    step S {
        var rendered = "hello"
        var response = Echo.run(prompt: rendered)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestUseMissingFile(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
use { Foo } from "./missing.mh"

pipeline P {
    step S {
        var value = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "missing.mh") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestPromptFromMarkdownFileResolvesWithoutFinding covers `prompt ... from
// "path"` (internal/lang/ast/prompt.go) resolving cleanly: lint's own import
// walk (internal/lang/lint/imports.go, deliberately mirroring
// internal/engine/interpreter's) loads the file and populates Body before
// checkAgentCalls ever runs, so a prompt referenced via `prompt:
// Greeting(...)` renders with no finding, same as an inline-body prompt.
func TestPromptFromMarkdownFileResolvesWithoutFinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, filepath.Join(dir, "greeting.prompt.md"), `hello ${name}`)
	write(t, main, `
prompt Greeting(name: string) from "./greeting.prompt.md"

agent Echo {
    command: "bash"
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting(name: "World"))
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestPromptFromMissingMarkdownFile is the failure path: a `from` path that
// doesn't resolve to a real file is reported the same way a broken
// `use`/`import` path is, instead of surfacing later as "prompt has no text
// body" once something tries to render it.
func TestPromptFromMissingMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
prompt Greeting(name: string) from "./missing.prompt.md"

pipeline P {
    step S {
        var value = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, "missing.prompt.md") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestUseSymbolNotExportedReportsEveryName(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	module := filepath.Join(dir, "module.mh")
	write(t, module, `
export agent Real {
    command: "bash"
}
`)
	write(t, main, `
use { Real, Ghost1, Ghost2 } from "./module.mh"

pipeline P {
    step S {
        var value = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}
	joined := findings[0].Message + " " + findings[1].Message
	if !strings.Contains(joined, `"Ghost1" is not exported`) || !strings.Contains(joined, `"Ghost2" is not exported`) {
		t.Errorf("expected both missing symbols reported, got: %+v", findings)
	}
}

func TestParseErrorBecomesFinding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `pipeline P { step { } }`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if findings[0].Line <= 0 {
		t.Errorf("expected a positive line number, got %d", findings[0].Line)
	}
}

func TestDirAggregatesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.mh"), `
pipeline A {
    step S {
        var response = GhostA.run(prompt: "hi")
    }
}
`)
	write(t, filepath.Join(dir, "b.mh"), `
pipeline B {
    step S {
        var response = GhostB.run(prompt: "hi")
    }
}
`)
	findings, err := lint.Dir(dir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}
	if !strings.HasSuffix(findings[0].File, "a.mh") || !strings.HasSuffix(findings[1].File, "b.mh") {
		t.Errorf("expected findings sorted by filename, got: %+v", findings)
	}
}

func TestDirValidProjectNoFindings(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.mh"), `
agent Reviewer {
    command: "bash"
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings, err := lint.Dir(dir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckPipelineContextUnknownKey(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline MainPipeline {
    context: {
        source: "latest"
        sauce: "typo"
    }

    step Step1 {
        var x = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `unknown key "sauce"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckPipelineContextUnrecognizedSource(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline MainPipeline {
    context: {
        source: "yesterday"
    }

    step Step1 {
        var x = 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `context source "yesterday" is not recognized`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckPipelineContextSessionSourceIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline MainPipeline {
    context: {
        source: "session:abc123"
    }

    step Step1 {
        var x = 1
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for source: \"session:<id>\", got: %+v", findings)
	}
}

func TestCheckPipelineContextStepReferenceIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline MainPipeline {
    context: {
        source: "latest"
    }

    step Step1 {
        log(context.session_id)
        log(context.vars)
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for a step reading context.*, got: %+v", findings)
	}
}

func TestCheckLoopStopWhenReferencesPipelineVar(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop pipeline MainPipeline {
    var count = 0

    repeat: {
        stop_when: count == 10
        max_iterations: 10
    }

    step Step1 {
        count = count + 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `stop_when references var "count"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckLoopStopWhenReferencesPipelineVarNestedInCall(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop pipeline MainPipeline {
    var count = 0

    repeat: {
        stop_when: count >= (5 + 5)
    }

    step Step1 {
        count = count + 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `stop_when references var "count"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckLoopStopWhenUsingMemoryIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory Session {
    type: "kv"
    store: "memory"
}

loop pipeline MainPipeline {
    repeat: {
        stop_when: Session.get("count", 0) == 10
        max_iterations: 10
    }

    step Step1 {
        Session.set("count", Session.get("count", 0) + 1)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckLoopStopWhenPlainPipelineNotFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    var count = 0

    step S {
        count = count + 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// A pipeline-level `mem` is a valid assignment target in a step, same as
// `var` — this must not false-positive "undefined variable".
func TestMemAssignmentInStepIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop pipeline MainPipeline {
    mem count = 0

    repeat: {
        stop_when: count == 10
        max_iterations: 10
    }

    step Step1 {
        count = count + 1
        count.reset()
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// checkLoopStopWhen must not flag a `mem` reference in stop_when — unlike
// `var`, that's the intended, supported usage.
func TestCheckLoopStopWhenMemNotFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop pipeline MainPipeline {
    mem count = 0

    repeat: {
        stop_when: count == 10
        max_iterations: 10
    }

    step Step1 {
        count = count + 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// --- agent retry/cache/rate_limit/fallback: static coverage lint previously
// lacked entirely (only runtime, in interpreter/agent.go, checked these) ---

func TestCheckAgentRetryMaxAttemptsNotANumber(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    retry: { max_attempts: "three" }
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" retry.max_attempts must be a number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAgentCacheTTLNotADuration(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    cache: { ttl: "forever" }
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" cache.ttl must be a duration`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAgentRateLimitRequestsPerMinuteNotANumber(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    rate_limit: { requests_per_minute: "many" }
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" rate_limit.requests_per_minute must be a number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAgentFallbackUndeclaredAgent(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    fallback: [Ghost]
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" fallback: agent "Ghost" is not declared`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckAgentRetryCacheRateLimitFallbackValidIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Backup {
    command: "bash"
}

agent Reviewer {
    command: "bash"
    retry: { max_attempts: 3, delay: 2s, retry_on: [500, "timeout"] }
    cache: { ttl: 1h, storage: "disk" }
    rate_limit: { requests_per_minute: 10, concurrency: 2, on_exceeded: "queue" }
    fallback: [Backup]
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckAgentRetryBackoffRejectsUnimplementedValue proves `retry.backoff`
// is validated, not silently accepted: traffic.Retrier only implements
// exponential backoff, so declaring any other value must be a lint finding
// instead of quietly running exponential anyway.
func TestCheckAgentRetryBackoffRejectsUnimplementedValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    retry: { max_attempts: 3, backoff: "linear" }
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" retry.backoff "linear" is not implemented`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckExtensionMCPProtocolRejectsUnknownValue proves an `extension mcp`
// declaration's `protocol:` is validated the same way retry.backoff is: only
// "auto", "2026-07-28", "2025-11-25", and "2025-06-18" are accepted, and
// anything else is a lint finding rather than a silent fall-through to auto.
func TestCheckExtensionMCPProtocolRejectsUnknownValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
extension mcp Wiki {
    transport: "stdio"
    command: "docker"
    protocol: "1999-01-01"
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `mcp "Wiki" protocol "1999-01-01" is not supported`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckExtensionMCPProtocolAcceptsEveryValidValue proves each of the four
// recognized `protocol:` values lints clean.
func TestCheckExtensionMCPProtocolAcceptsEveryValidValue(t *testing.T) {
	for _, v := range []string{"auto", "2026-07-28", "2025-11-25", "2025-06-18"} {
		dir := t.TempDir()
		main := filepath.Join(dir, "main.mh")
		write(t, main, `
extension mcp Wiki {
    transport: "stdio"
    command: "docker"
    protocol: "`+v+`"
}
`)
		if findings := lint.File(main); len(findings) != 0 {
			t.Errorf("protocol %q: expected 0 findings, got %+v", v, findings)
		}
	}
}

// TestCheckAgentCacheStrategyRejectsUnimplementedValue proves
// `cache.strategy` is validated: traffic.Cache only implements exact-match
// caching, so declaring any other strategy must be a lint finding instead
// of silently falling back to exact-match.
func TestCheckAgentCacheStrategyRejectsUnimplementedValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    cache: { ttl: 1h, strategy: "semantic" }
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" cache.strategy "semantic" is not implemented`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckAgentRateLimitOnExceededRejectsUnimplementedValue proves
// `rate_limit.on_exceeded` is validated: traffic.Limiter only implements
// block-and-wait ("queue"), so declaring any other value must be a lint
// finding instead of silently queuing anyway.
func TestCheckAgentRateLimitOnExceededRejectsUnimplementedValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer {
    command: "bash"
    rate_limit: { concurrency: 2, on_exceeded: "reject" }
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `agent "Reviewer" rate_limit.on_exceeded "reject" is not implemented`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckLoopStopWhenUnrelatedVarNotFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop pipeline MainPipeline {
    var count = 0

    repeat: {
        stop_when: max_iterations_reached == true
        max_iterations: 10
    }

    step Step1 {
        count = count + 1
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckParamDefaultsRequiredAfterOptional(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool T {
    bad(a: string = "x", b: string) -> a + b
    ok(a: string, b: string = "y", c: string = "z") -> a + b + c
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `parameter "b" has no default but follows a defaulted parameter`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestCheckToolCallOmittingDefaultedArgIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool T {
    greet(name: string, greeting: string = "Hi") -> greeting + name
}
pipeline P {
    step S {
        T.greet("world")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}
