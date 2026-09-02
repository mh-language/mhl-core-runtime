package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestSpawnCleanPipeline(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer { command: "echo" args: ["ok"] }

pipeline P {
    spawn: { max_concurrency: 2 }
    step S {
        spawn rev = Reviewer.run(prompt: "check this")
        wait rev timeout: 30s
        log(rev.result)
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestSpawnRHSMustBeAgentRun(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step S {
        spawn x = 1 + 2
        wait x
    }
}
`)
	findings := lint.File(main)
	if len(findings) == 0 || !strings.Contains(findings[0].Message, "must be an <Agent>.run(...) call") {
		t.Fatalf("expected an <Agent>.run finding, got %+v", findings)
	}
}

func TestWaitUnknownHandle(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent A { command: "echo" args: ["ok"] }

pipeline P {
    step S {
        spawn a = A.run(prompt: "x")
        wait a, ghost
    }
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, `"ghost" is not a spawned handle`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unknown-handle finding, got %+v", findings)
	}
}

// The fan-out form lints clean: the `for item in ...` loop variable is in
// scope for the prompt, and the handle-array name is a known `wait` target.
func TestSpawnFanOutCleanPipeline(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer { command: "echo" args: ["ok"] }

pipeline P {
    spawn: { max_concurrency: 2 }
    step S {
        var angles = ["a", "b"]
        spawn reviews = Reviewer.run(prompt: "on ${item}") for item in angles
        wait reviews timeout: 30s
        log(reviews.size())
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestSpawnRejectedInToolMethod(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent A { command: "echo" args: ["ok"] }

tool T {
    go(): number -> {
        spawn a = A.run(prompt: "x")
        return 1
    }
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "spawn is only valid inside a pipeline step") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a step-only finding, got %+v", findings)
	}
}
