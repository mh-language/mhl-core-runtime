package execsvc_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	mhlruntime "github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
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

// Request.Principal surfaces to a pipeline that declares a `context:` block as
// the read-only identifier context.principal; "" when unset.
func TestRunExposesPrincipal(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    context: {}
    var who = ""
    step S { who = context.principal }
}
`)
	got := func(principal string) any {
		res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: t.TempDir(), Principal: principal})
		if err != nil {
			t.Fatalf("run(%q): %v", principal, err)
		}
		return res.Vars["who"]
	}
	if v := got("alice@acme.com"); v != "alice@acme.com" {
		t.Errorf("context.principal = %v, want alice@acme.com", v)
	}
	if v := got(""); v != "" {
		t.Errorf("context.principal with no principal = %v, want empty", v)
	}
}

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

// A step that declares `timeout <dur>` self-terminates when it runs over,
// with no run-level cancel — the derived per-step context aborts the
// in-flight cmd.exec and Run surfaces runtime.ErrStepTimeout.
func TestRunStepTimeoutAbortsInFlightCmdExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX `sleep`")
	}
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    step Slow timeout 200ms { var r = cmd.exec(["sleep", "30"]) }
}
`)

	start := time.Now()
	_, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
	elapsed := time.Since(start)

	if !errors.Is(err, mhlruntime.ErrStepTimeout) {
		t.Fatalf("err = %v, want runtime.ErrStepTimeout", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("run took %s — the step timeout did not abort the sleep", elapsed)
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

// The pipeline's InputSchema is enforced before any step runs: a missing
// required input, or an undeclared one, is an *runtime.InvalidInputsError —
// never a silent no-op or a late "undefined variable".
func TestRunEnforcesInputSchema(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    input repo: string
    input approved: string
    step S { var x = repo + approved }
}
`)
	cases := []struct {
		name    string
		inputs  map[string]any
		missing []string
		unknown []string
	}{
		{"missing required", map[string]any{"approved": "yes"}, []string{"repo"}, nil},
		{"undeclared key", map[string]any{"repo": "r", "approved": "y", "extra": 1}, nil, []string{"extra"}},
		{"both", map[string]any{"nope": 1}, []string{"approved", "repo"}, []string{"nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execsvc.Run(execsvc.Request{Source: src, Inputs: tc.inputs, BaseDir: dir})
			var ie *mhlruntime.InvalidInputsError
			if !errors.As(err, &ie) {
				t.Fatalf("err = %v, want *runtime.InvalidInputsError", err)
			}
			if !slicesEqual(ie.Missing, tc.missing) {
				t.Errorf("Missing = %v, want %v", ie.Missing, tc.missing)
			}
			if !slicesEqual(ie.Unknown, tc.unknown) {
				t.Errorf("Unknown = %v, want %v", ie.Unknown, tc.unknown)
			}
		})
	}
}

// A resume trusts the checkpoint for inputs, so schema admission is skipped:
// `mhl run --resume` with no --input flags must not trip "missing required".
func TestRunResumeSkipsInputSchema(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
pipeline P {
    input repo: string
    checkpoint { strategy: "per_step" }
    step S { var x = repo }
}
`)
	// No prior checkpoint exists; Resume still must get past admission and
	// fail later (nothing to resume), not at the InputSchema check.
	_, err := execsvc.Run(execsvc.Request{Source: src, Inputs: nil, BaseDir: dir, Resume: true})
	var ie *mhlruntime.InvalidInputsError
	if errors.As(err, &ie) {
		t.Fatalf("resume tripped InputSchema admission: %v", err)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
