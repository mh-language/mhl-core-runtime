package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestParallelCleanPipeline(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Docs { command: "echo" args: ["ok"] }

pipeline P {
    var a = ""
    var b = ""
    parallel Gather {
        step FetchA { a = Docs.run(prompt: "a") }
        step FetchB { b = Docs.run(prompt: "b") }
    }
    step Merge { log("${a} ${b}") }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestParallelGotoInsideBranchFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    parallel G {
        step One { goto Two }
        step Two { log("two") }
    }
    step S { log("s") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, "`goto` cannot be used inside a parallel group") {
		t.Fatalf("expected a goto-in-branch finding, got %+v", findings)
	}
}

func TestParallelBreakInsideBranchFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    parallel G {
        step One { break "stop" }
        step Two { log("two") }
    }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, "`break` cannot be used inside a parallel group") {
		t.Fatalf("expected a break-in-branch finding, got %+v", findings)
	}
}

func TestParallelGotoTargetingGroupedStepFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step Start { goto Two }
    parallel G {
        step One { log("one") }
        step Two { log("two") }
    }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, "targets a step inside a parallel group") {
		t.Fatalf("expected a goto-into-group finding, got %+v", findings)
	}
}

func TestParallelDuplicateStepNameFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step Dup { log("plain") }
    parallel G {
        step Dup { log("grouped") }
        step Other { log("other") }
    }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `declares more than one step named "Dup"`) {
		t.Fatalf("expected a duplicate-step-name finding, got %+v", findings)
	}
}

// `goto` between two plain steps is still fine when a group exists elsewhere.
func TestParallelGotoBetweenPlainStepsStillOK(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    parallel G {
        step A { log("a") }
        step B { log("b") }
    }
    step C { goto E }
    step D { log("d") }
    step E { log("e") }
}
`)
	findings := lint.File(main)
	for _, f := range findings {
		if strings.Contains(f.Message, "parallel") || strings.Contains(f.Message, "goto") {
			t.Fatalf("unexpected finding: %+v", f)
		}
	}
}

func hasMessage(findings []lint.Finding, substr string) bool {
	for _, f := range findings {
		if strings.Contains(f.Message, substr) {
			return true
		}
	}
	return false
}
