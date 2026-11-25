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
    step S {
        spawn rev = Reviewer.run(prompt: "check this")
        wait rev timeout 30s
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
    step S {
        var angles = ["a", "b"]
        spawn reviews concurrency 2 = Reviewer.run(prompt: "on ${item}") for item in angles
        wait reviews timeout 30s
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

// The deprecated pipeline-wide `spawn: { max_concurrency }` block still
// works but is reported as a warning pointing at the statement clause.
func TestSpawnPipelineBlockIsDeprecated(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
agent Reviewer { command: "echo" args: ["ok"] }

pipeline P {
    spawn: { max_concurrency: 2 }
    step S {
        spawn rev = Reviewer.run(prompt: "check this")
        wait rev
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 || findings[0].Severity != lint.SeverityWarning ||
		!strings.Contains(findings[0].Message, "is deprecated") || !strings.Contains(findings[0].Message, "concurrency N") {
		t.Fatalf("expected one deprecation warning, got %+v", findings)
	}
}

func TestSpawnConcurrencyClause(t *testing.T) {
	cases := []struct {
		name     string
		stmt     string
		want     string // substring of the only finding; "" = clean
		severity lint.Severity
	}{
		{"fan-out", `spawn xs concurrency 2 = Reviewer.run(prompt: "on ${i}") for i in items`, "", ""},
		{"zero", `spawn xs concurrency 0 = Reviewer.run(prompt: "on ${i}") for i in items`, `concurrency "0" is not a positive integer`, lint.SeverityError},
		{"fraction", `spawn xs concurrency 1.5 = Reviewer.run(prompt: "on ${i}") for i in items`, `concurrency "1.5" is not a positive integer`, lint.SeverityError},
		{"single spawn", `spawn xs concurrency 2 = Reviewer.run(prompt: "once")`, "no effect on a single spawn", lint.SeverityWarning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.mh")
			write(t, main, `
agent Reviewer { command: "echo" args: ["ok"] }

pipeline P {
    step S {
        var items = ["a", "b"]
        `+tc.stmt+`
        wait xs
    }
}
`)
			findings := lint.File(main)
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("expected no findings, got %+v", findings)
				}
				return
			}
			if len(findings) != 1 || !strings.Contains(findings[0].Message, tc.want) || findings[0].Severity != tc.severity {
				t.Fatalf("expected one %q finding (%q), got %+v", tc.want, tc.severity, findings)
			}
		})
	}
}

func TestWaitOptions(t *testing.T) {
	cases := []struct {
		name     string
		wait     string
		want     string // substring of the only finding; "" = clean
		severity lint.Severity
	}{
		{"clauses", `wait xs timeout 30s collect`, "", ""},
		{"timeout colon", `wait xs timeout: 30s`, "`timeout: <duration>` is deprecated", lint.SeverityWarning},
		{"on_error", `wait xs on_error: "collect"`, "`on_error: \"collect\"` is deprecated", lint.SeverityWarning},
		{"on_error value", `wait xs on_error: "ignore"`, `on_error only accepts "collect"`, lint.SeverityError},
		{"timeout not a duration", `wait xs timeout "soon"`, "timeout must be a duration literal", lint.SeverityError},
		{"collect with any", `wait any xs collect`, "collect only applies to a plain `wait`", lint.SeverityError},
		{"collect with quorum", `wait 1 of xs collect`, "collect only applies to a plain `wait`", lint.SeverityError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.mh")
			write(t, main, `
agent Reviewer { command: "echo" args: ["ok"] }

pipeline P {
    step S {
        spawn xs = Reviewer.run(prompt: "on ${i}") for i in ["a", "b"]
        `+tc.wait+`
    }
}
`)
			findings := lint.File(main)
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("expected no findings, got %+v", findings)
				}
				return
			}
			if len(findings) != 1 || !strings.Contains(findings[0].Message, tc.want) || findings[0].Severity != tc.severity {
				t.Fatalf("expected one %q finding (%q), got %+v", tc.want, tc.severity, findings)
			}
		})
	}
}
