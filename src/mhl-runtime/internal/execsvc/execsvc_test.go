package execsvc_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const twoPipelines = `
pipeline First {
    input name: string
    var greeting = ""
    step Build { greeting = "hi " + name }
}

workflow Second {
    var n = 0
    step A { n = 1 }
    step B { if (n == 1) goto Done }
    step C { n = 99 }
    step Done { n = n + 1 }
}
`

// Run from a source path returns the final variable state and the steps that
// executed, exactly as `mhl run` would drive it.
func TestRunFromSource(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", twoPipelines)

	res, err := execsvc.Run(execsvc.Request{
		Source:  src,
		Inputs:  map[string]any{"name": "ana"},
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PipelineName != "First" {
		t.Errorf("PipelineName = %q, want First (the first declared)", res.PipelineName)
	}
	if got := res.Vars["greeting"]; got != "hi ana" {
		t.Errorf("Vars[greeting] = %v, want %q", got, "hi ana")
	}
	if len(res.Executed) != 1 || res.Executed[0] != "Build" {
		t.Errorf("Executed = %v, want [Build]", res.Executed)
	}
	if res.SessionID == "" {
		t.Error("expected a resolved SessionID")
	}
}

// Workflow selects a declaration by name, and a workflow's `goto` still
// drives the step sequence.
func TestRunSelectsWorkflowByName(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", twoPipelines)

	res, err := execsvc.Run(execsvc.Request{
		Source:   src,
		Workflow: "Second",
		BaseDir:  dir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PipelineName != "Second" {
		t.Fatalf("PipelineName = %q, want Second", res.PipelineName)
	}
	// A -> B -> (goto Done), skipping C.
	if got := res.Vars["n"]; got != float64(2) {
		t.Errorf("Vars[n] = %v, want 2 (C skipped via goto)", got)
	}
}

// A pre-parsed program (the server preload path) runs without re-reading the
// file.
func TestRunFromPreparsedProgram(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", twoPipelines)
	prog, err := parser.Parse(twoPipelines)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	res, err := execsvc.Run(execsvc.Request{
		Program: prog,
		File:    src,
		Inputs:  map[string]any{"name": "bo"},
		BaseDir: dir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Vars["greeting"] != "hi bo" {
		t.Errorf("Vars[greeting] = %v, want %q", res.Vars["greeting"], "hi bo")
	}
}

// A cancelled Request.Context stops the run at its first step boundary.
func TestRunHonorsCancelledContext(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    step A { log("a") }
    step B { log("b") }
}
`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Context: ctx})
	if err == nil {
		t.Fatal("expected a cancellation error from a pre-cancelled context")
	}
}

// A run-level cancel aborts a blocking native op already in flight — not
// just at the next step boundary (increment 2b: ctx threads into RunStep
// and down to exec.CommandContext).
func TestRunCancelAbortsInFlightCmdExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX `sleep`")
	}
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    step Slow { var r = cmd.exec(["sleep", "30"]) }
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir, Context: ctx})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the cancelled cmd.exec to surface an error")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("run took %s — the sleep was not aborted by the context", elapsed)
	}
}

// A malformed input value is rejected before any step runs.
func TestRunRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    input count: number
    step S { count = count + 1 }
}
`)
	_, err := execsvc.Run(execsvc.Request{
		Source:  src,
		Inputs:  map[string]any{"count": "not-a-number"},
		BaseDir: dir,
	})
	if err == nil {
		t.Fatal("expected an error coercing count=not-a-number against `input count: number`")
	}
}
