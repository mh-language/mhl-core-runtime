package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestRunPartialWorkflowSplitAcrossFiles is the motivating case for
// `partial`: a workflow's steps split across two files, with the primary
// file's `entry step` chosen as the real starting point regardless of
// declaration order, and a `goto` from the entry step reaching a step
// declared in the other file (only reachable because that file was pulled
// in with a whole-file `import "..."`, not the named `{ }` form).
func TestRunPartialWorkflowSplitAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	fragment := `
partial workflow Discovery {
    step Gate {
        log("gate reached")
    }
}
`
	main := `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        log("dispatch reached")
        goto Gate
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.mh"), []byte(fragment), 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	mainFile := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(mainFile, []byte(main), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", mainFile}, &buf); err != nil {
		t.Fatalf("run: %v\noutput:\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "dispatch reached") || !strings.Contains(out, "gate reached") {
		t.Fatalf("expected both steps to run, got:\n%s", out)
	}
	if strings.Index(out, "dispatch reached") > strings.Index(out, "gate reached") {
		t.Fatalf("expected Dispatch (entry) to run before Gate, got:\n%s", out)
	}
}

// TestRunPartialWorkflowHonorsEntryOverMergeOrder is the case that would
// break without PipelineFromAST's reordering: the *fragment* file (pulled
// in second, appended after the primary's own Body) is the one declaring
// `entry step Dispatch`, while the primary file's own step (Gate) ends up
// physically first in the merged Body purely because it's the file `mhl
// run` was pointed at. Without honoring `entry`, the runner would start at
// Gate (Steps[0] pre-reorder) and never reach Dispatch at all.
func TestRunPartialWorkflowHonorsEntryOverMergeOrder(t *testing.T) {
	dir := t.TempDir()
	fragment := `
partial workflow Discovery {
    entry step Dispatch {
        log("dispatch reached")
        goto Gate
    }
}
`
	main := `
import "fragment.mh"

partial workflow Discovery {
    step Gate {
        log("gate reached")
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.mh"), []byte(fragment), 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	mainFile := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(mainFile, []byte(main), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", mainFile}, &buf); err != nil {
		t.Fatalf("run: %v\noutput:\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "dispatch reached") || !strings.Contains(out, "gate reached") {
		t.Fatalf("expected both steps to run, got:\n%s", out)
	}
	if strings.Index(out, "dispatch reached") > strings.Index(out, "gate reached") {
		t.Fatalf("expected entry step Dispatch to run before Gate despite merge order, got:\n%s", out)
	}
}

// TestRunPartialWorkflowMissingEntryFails is the interpreter-side
// counterpart to the lint tests in internal/lang/lint: resolvePartials
// (interpreter.ResolveImports) must itself refuse to run a merged partial
// with zero entry steps, not just have `mhl lint` flag it — a run that
// skipped lint shouldn't silently fall back to "first step in merge order".
func TestRunPartialWorkflowMissingEntryFails(t *testing.T) {
	dir := t.TempDir()
	fragment := `
partial workflow Discovery {
    step Gate {
        log("gate reached")
    }
}
`
	main := `
import "fragment.mh"

partial workflow Discovery {
    step Dispatch {
        goto Gate
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.mh"), []byte(fragment), 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	mainFile := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(mainFile, []byte(main), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", mainFile}, &buf)
	if err == nil {
		t.Fatalf("expected an error, got success:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "expected exactly one `entry step`") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTestDirWithLoneFragmentDoesNotFailOtherFiles mirrors
// TestLoadDirectoryWithPartialFragmentDoesNotFailOtherWorkflows one layer
// up: `mhl test <dir>` (runTestFile) also resolves every .mh file's imports
// independently, so a lone `partial` fragment — no test block of its own,
// its siblings not pulled in from where it sits — must not abort the whole
// directory's test run. Before resolvePartials stopped hard-erroring on a
// merged (but, alone, incomplete) partial at import-resolution time, this
// failed the entire `mhl test dir` run just because fragment.mh happened to
// sit next to main.mh, regardless of main.mh's own test block being
// perfectly valid.
func TestTestDirWithLoneFragmentDoesNotFailOtherFiles(t *testing.T) {
	dir := t.TempDir()
	fragment := `
partial workflow Discovery {
    step Gate {
        log("gate reached")
    }
}
`
	main := `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        goto Gate
    }
}

test t {
    describe entry_wins {
        var result = Discovery.run()
        is_true(result.ok)
        are_equal(result.executed, ["Dispatch", "Gate"])
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.mh"), []byte(fragment), 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.mh"), []byte(main), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"test", dir}, &buf); err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, buf.String())
	}
	if out := buf.String(); !strings.Contains(out, "2 passed, 0 failed, 0 incomplete") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// TestRunPartialWorkflowCompleteDoesNotNeedToBeLast is complete()'s payoff
// for `partial`: with it, a convergence step no longer has to live in
// whichever fragment happens to be imported last — the whole reason the
// fallthrough lint check (internal/lang/lint/fallthrough.go) and this test
// file's other cases exist. Here Gate (in the *first*-merged fragment,
// main.mh) calls complete() directly, with a second fragment still merged
// after it — and the run still stops exactly there.
func TestRunPartialWorkflowCompleteDoesNotNeedToBeLast(t *testing.T) {
	dir := t.TempDir()
	fragment := `
partial workflow Discovery {
    step NeverReached {
        log("should not run")
    }
}
`
	main := `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        log("dispatch reached")
        goto Gate
    }
    step Gate {
        log("gate reached")
        complete()
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.mh"), []byte(fragment), 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}
	mainFile := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(mainFile, []byte(main), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", mainFile}, &buf); err != nil {
		t.Fatalf("run: %v\noutput:\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "dispatch reached") || !strings.Contains(out, "gate reached") {
		t.Fatalf("expected both steps to run, got:\n%s", out)
	}
	if strings.Contains(out, "should not run") {
		t.Fatalf("expected NeverReached to never run, got:\n%s", out)
	}
}
