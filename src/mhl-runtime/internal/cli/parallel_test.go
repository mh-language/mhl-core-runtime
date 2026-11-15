package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

func runParallel(t *testing.T, src string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "p.mh")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	var buf bytes.Buffer
	err := cli.Run(append([]string{"run", "p.mh"}, args...), &buf)
	return buf.String(), err
}

// A parallel group runs both branch steps, prints their captured output in
// declared order (not completion order), then the following step sees both
// branches' merged var writes.
func TestParallelGroupRunsBranchesAndMerges(t *testing.T) {
	src := `
agent Fast { command: "sh" args: ["-c", "echo fast"] }
agent Slow { command: "sh" args: ["-c", "sleep 0.3; echo slow"] }

pipeline P {
    input topic: string
    var docs = ""
    var issues = ""

    parallel Gather {
        step FetchDocs   { docs = Fast.run(prompt: "${topic}") }
        step FetchIssues { issues = Slow.run(prompt: "${topic}") }
    }

    step Merge {
        log("docs=${docs}")
        log("issues=${issues}")
    }
}
`
	out, err := runParallel(t, src, "--input", "topic=x")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	// Declared order: FetchDocs' "step:" line precedes FetchIssues', even
	// though FetchIssues' agent finishes much later.
	di, ii := strings.Index(out, "step: FetchDocs"), strings.Index(out, "step: FetchIssues")
	if di < 0 || ii < 0 || di > ii {
		t.Fatalf("branch output not in declared order:\n%s", out)
	}
	if !strings.Contains(out, "docs=fast") || !strings.Contains(out, "issues=slow") {
		t.Fatalf("Merge did not see both branch writes:\n%s", out)
	}
	if !strings.Contains(out, "executed 3 step(s)") {
		t.Fatalf("want 3 executed steps:\n%s", out)
	}
}

// Two branches writing the same pipeline var to different values fails the run.
func TestParallelConflictingWritesFailRun(t *testing.T) {
	src := `
agent A { command: "sh" args: ["-c", "echo x"] }

pipeline P {
    var shared = ""
    parallel G {
        step One { shared = "from-one" }
        step Two { shared = "from-two" }
    }
    step S { log(shared) }
}
`
	out, err := runParallel(t, src)
	if err == nil {
		t.Fatalf("expected a conflict error, got none:\n%s", out)
	}
	if !strings.Contains(err.Error(), "both assigned pipeline var") {
		t.Fatalf("error = %v", err)
	}
}

// A `merge (branches) { ... }` block combines a var two branches both
// assigned — what is otherwise a conflict error — reading each branch's
// value through the branches object, while vars the branches agree on are
// already merged when it runs.
func TestParallelMergeCombinesConflictingWrites(t *testing.T) {
	src := `
workflow W {
    var reply = []
    var n = 0
    parallel Fan {
        step Left  { self.reply = [...self.reply, "claude"]  self.n = 1 }
        step Right { self.reply = [...self.reply, "codex"] }
        merge (branches) {
            self.reply = [...branches.Left.reply, ...branches.Right.reply]
            log("n=${n}")
        }
    }
    step Done {
        log("reply=${reply}")
    }
}
`
	out, err := runParallel(t, src)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"step: Fan.merge", "n=1", `reply=["claude","codex"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "step: Right") > strings.Index(out, "step: Fan.merge") {
		t.Errorf("merge ran before the barrier:\n%s", out)
	}
}

// A merge that leaves a conflicting var unassigned fails the group rather
// than silently dropping both branches' values.
func TestParallelMergeMustAssignConflicts(t *testing.T) {
	src := `
workflow W {
    var reply = []
    parallel Fan {
        step Left  { self.reply = ["a"] }
        step Right { self.reply = ["b"] }
        merge (branches) { log("noop") }
    }
}
`
	out, err := runParallel(t, src)
	if err == nil || !strings.Contains(err.Error(), "its merge did not assign it") {
		t.Fatalf("want an unassigned-conflict error, got %v\n%s", err, out)
	}
}
