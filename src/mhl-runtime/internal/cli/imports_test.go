package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

const transitiveUseMemoryFile = `
export memory Store {
    type: "kv"
    store: "memory"
}
`

const transitiveUseToolFile = `
use {Store} from "memory.mh"

export tool Counter {
    pending() -> {
        return Store.get("n", 0)
    }
}
`

const transitiveUsePipelineFile = `
use {Counter} from "tool.mh"

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
use {Counter} from "tool.mh"
use {Store} from "memory.mh"

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
