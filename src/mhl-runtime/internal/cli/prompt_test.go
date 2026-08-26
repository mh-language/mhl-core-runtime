package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestRunWithDynamicPrompt exercises language-design.md §2 "Prompts
// Dinâmicos" end-to-end: a `prompt Name(params) { "...${param}..." }`
// declaration referenced from `.run(prompt: Name(args...))` is rendered and
// the resulting text is what actually reaches the agent's command line.
func TestRunWithDynamicPrompt(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
prompt Greeting(name: string) {
    "Hello, ${name}!"
}

agent Echo {
    command: "echo"
    trace: true
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting(name: "World"))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Hello, World!") {
		t.Errorf("output missing rendered prompt: %s", out)
	}
}

// TestRunWithNestedDynamicPrompt exercises composing one `prompt` from
// another: an argument value that is itself a `Name(...)` reference to a
// declared prompt is rendered first and substituted in as plain text, so
// prompts can be built up out of smaller, reusable prompts instead of
// duplicating shared text across templates.
func TestRunWithNestedDynamicPrompt(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
prompt Role(title: string) {
    "a ${title} expert"
}

prompt Greeting(role: prompt, name: string) {
    "You are ${role}. Hello, ${name}!"
}

agent Echo {
    command: "echo"
    trace: true
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting(role: Role(title: "security"), name: "World"))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "You are a security expert. Hello, World!") {
		t.Errorf("output missing rendered nested prompt: %s", out)
	}
}

func TestRunWithDynamicPromptNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent Echo {
    command: "echo"
    trace: true
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Ghost(name: "World"))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a reference to an undeclared prompt")
	}
	if !strings.Contains(err.Error(), `prompt "Ghost" not found`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunWithDynamicPromptMissingParam(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
prompt Greeting(name: string) {
    "Hello, ${name}!"
}

agent Echo {
    command: "echo"
    trace: true
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting())
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a missing required prompt parameter")
	}
	if !strings.Contains(err.Error(), `missing value for parameter "name"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunWithDynamicPromptAcrossImport confirms an `export prompt` declared
// in one module and pulled in via `use { Name } from "..."` is resolved and
// rendered exactly like a locally declared prompt.
func TestRunWithDynamicPromptAcrossImport(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "module.mh")
	if err := os.WriteFile(module, []byte(`
export prompt Greeting(name: string) {
    "Hello, ${name}!"
}
`), 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
use { Greeting } from "./module.mh"

agent Echo {
    command: "echo"
    trace: true
}

pipeline P {
    step S {
        var response = Echo.run(prompt: Greeting(name: "World"))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "Hello, World!") {
		t.Errorf("output missing rendered prompt: %s", buf.String())
	}
}

// TestPromptCallAsOrdinaryExpression proves a prompt reference is no longer
// confined to the literal `prompt:` argument of an agent's own .run(...)
// call: `Greeting(...)` now renders through the same evalPostfix dispatch
// any other call goes through, so it can be assigned to a var, concatenated
// with another string, or passed as a plain positional argument to a tool
// method — none of which required special-casing on the tool's side.
func TestPromptCallAsOrdinaryExpression(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
prompt Greeting(name: string) {
    "Hello, ${name}!"
}

tool Wrapper {
    describe(text) -> "wrapped: ${text}"
}

pipeline P {
    step S {
        var rendered = Greeting(name: "World")
        log(rendered)
        log(Wrapper.describe(Greeting(name: "Tool")))
        log("> " + Greeting(name: "Concat"))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Hello, World!", "wrapped: Hello, Tool!", "> Hello, Concat!"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

// TestPromptCallArgumentAcceptsRuntimeExpression proves a prompt template's
// own arguments are ordinary expressions now, not just a literal string
// token or a bare nested prompt reference — `feature.title` here is a
// field read off a runtime value, the exact shape development.pipe.mh's
// commented-out Implement step needed and couldn't have (see its "SKETCH
// GAP"-style comment) before this dispatch existed.
func TestPromptCallArgumentAcceptsRuntimeExpression(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
prompt FixPrompt(title: string) {
    "Corrija a feature: ${title}"
}

agent Echo {
    command: "echo"
    trace: true
}

pipeline P {
    step S {
        var feature = {"title": "checkout"}
        var response = Echo.run(prompt: FixPrompt(title: feature.title))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "Corrija a feature: checkout") {
		t.Errorf("output missing rendered prompt: %s", buf.String())
	}
}

// TestRunWithPromptArgumentAsVariable proves `prompt:` itself now accepts
// any expression evaluating to a string, not just a literal or a bare
// `Name(...)` reference — a prompt pre-rendered into a variable earlier
// (e.g. by a tool method that received it as a plain parameter, the
// original CodexAgentAdapter.execute(prompt, schema) motivation) can be
// forwarded through `.run(prompt: thatVariable)`.
func TestRunWithPromptArgumentAsVariable(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
prompt Greeting(name: string) {
    "Hello, ${name}!"
}

agent Echo {
    command: "echo"
    trace: true
}

tool Adapter {
    execute(prompt) -> Echo.run(prompt: prompt)
}

pipeline P {
    step S {
        var response = Adapter.execute(Greeting(name: "World"))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "Hello, World!") {
		t.Errorf("output missing rendered prompt: %s", buf.String())
	}
}

// TestPromptCallUnknownNameFallsThroughToVariableLookup proves a bare
// `Name(...)` call whose Name doesn't match any declared prompt is left
// alone by the new dispatch — it falls through to the ordinary "is this a
// closure-holding variable" path (callClosure) exactly as before this
// feature existed, so `predicate(item)` for a lambda-holding variable is
// unaffected.
func TestPromptCallUnknownNameFallsThroughToVariableLookup(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
pipeline P {
    step S {
        var double = (x) -> x * 2
        log(double(21))
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "42") {
		t.Errorf("output missing closure call result: %s", buf.String())
	}
}
