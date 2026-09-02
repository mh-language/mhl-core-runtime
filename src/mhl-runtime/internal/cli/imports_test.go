package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const transitiveUseMemoryFile = `
export memory Store {
    type: "kv"
    store: "memory"
}
`

const transitiveUseToolFile = `
import {Store} from "memory.mh"

export tool Counter {
    pending() -> {
        return Store.get("n", 0)
    }
}
`

const transitiveUsePipelineFile = `
import {Counter} from "tool.mh"

pipeline P {
    step S {
        log("pending=${Counter.pending()}")
    }
}
`

// TestRunTransitiveUseResolvesThroughIntermediateModule is the case
// internal/engine/interpreter.ResolveImports' transitivity exists for: a
// pipeline `use`s a tool from one file, and that tool's own method body
// depends on a memory declared in a *third* file, reached only through the
// tool's own (separate) `use`. Before ResolveImports resolved imports
// recursively, only the tool declaration itself made it into the running
// program — Store was never merged in, so Counter.pending() failed at run
// time with `memory "Store" not found`, even though nothing in
// pipeline.mh ever names Store directly.
func TestRunTransitiveUseResolvesThroughIntermediateModule(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"memory.mh":   transitiveUseMemoryFile,
		"tool.mh":     transitiveUseToolFile,
		"pipeline.mh": transitiveUsePipelineFile,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "pending=0") {
		t.Errorf("output missing %q:\n%s", "pending=0", out)
	}
}

// TestRunTransitiveUseDiamondDoesNotDuplicateDeclarations covers the case
// resolveImports' visiting set and declPresent dedupe exist for: pipeline.mh
// uses both Counter (which itself transitively pulls in Store) *and* Store
// directly — a diamond. It must resolve exactly once, with no duplicate-
// declaration error or double-counted state.
func TestRunTransitiveUseDiamondDoesNotDuplicateDeclarations(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"memory.mh": transitiveUseMemoryFile,
		"tool.mh":   transitiveUseToolFile,
		"pipeline.mh": `
import {Counter} from "tool.mh"
import {Store} from "memory.mh"

pipeline P {
    step S {
        Store.set("n", 5)
        log("pending=${Counter.pending()}")
    }
}

`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "pending=5") {
		t.Errorf("output missing %q:\n%s", "pending=5", out)
	}
}

func TestRunUseAliasResolvesTopLevelAndTransitiveReferences(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"memory.mh": `
export memory Store {
    type: "kv"
    store: "memory"
}
`,
		"tool.mh": `
import {Store as store} from "memory.mh"

export tool Counter {
    pending() -> store.get("n", 0)
}
`,
		"pipeline.mh": `
import {Counter as counter} from "tool.mh"

pipeline P {
    step S {
        counter.pending()
        log(store.set("n", 7))
        log(counter.pending())
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "7") {
		t.Errorf("output missing aliased memory result 7:\n%s", out)
	}
}

// TestRunPromptFromMarkdownFileRenders covers `prompt ... from "path"`
// (internal/lang/ast/prompt.go): the body is loaded from an external
// Markdown file during import resolution — relative to the .mh file that
// declares it, same as `use`/`import` — and behaves exactly like an inline
// """...""" body from there on: ${name} still substitutes, and a \${...}
// escape (only meaningful in Markdown pulled in from elsewhere, which is
// far more likely to contain incidental ${...} than a hand-written inline
// template) renders as a literal placeholder instead of erroring on an
// undeclared parameter.
func TestRunPromptFromMarkdownFileRenders(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"greeting.prompt.md": "hello ${name}, run with \\${TARGET_DIR}",
		"prompt.mh": `
export prompt Greeting(name: string) from "greeting.prompt.md"
`,
		"pipeline.mh": `
import {Greeting} from "prompt.mh"

pipeline P {
    step S {
        log(Greeting(name: "World"))
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "hello World, run with ${TARGET_DIR}") {
		t.Errorf("output missing rendered markdown prompt:\n%s", out)
	}
}

// TestRunPromptFromMissingMarkdownFileFails is the failure path: a `from`
// path that doesn't resolve to a real file must surface as a run error, the
// same way a broken `use`/`import` path does, instead of silently producing
// an empty or partial prompt body.
func TestRunPromptFromMissingMarkdownFileFails(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"prompt.mh": `
export prompt Greeting(name: string) from "missing.prompt.md"
`,
		"pipeline.mh": `
import {Greeting} from "prompt.mh"

pipeline P {
    step S {
        log(Greeting(name: "World"))
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh"}, &buf)
	if err == nil {
		t.Fatalf("expected an error for a missing prompt source file")
	}
	if !strings.Contains(err.Error(), "Greeting") || !strings.Contains(err.Error(), "missing.prompt.md") {
		t.Errorf("expected error to name the prompt and missing path, got: %v", err)
	}
}
