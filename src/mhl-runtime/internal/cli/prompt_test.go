package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
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
