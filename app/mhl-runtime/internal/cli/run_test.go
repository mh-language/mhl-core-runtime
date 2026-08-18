package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

const pipelineFile = `
pipeline AutoFixPipeline {
    input issue_id: string

    checkpoint: {
        enabled: true
        strategy: "per_step"
        storage: "file"
        ttl: 7d
    }

    step AuditWithSkill {
        var x = 1
    }

    step RefinementLoop {
        var y = 2
    }
}
`

// IF-1: `mhl run <pipeline.mhl>` executes a pipeline from the start; a
// subsequent completed run clears its checkpoint.
func TestRunPipelineFromStart(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mhl")
	if err := os.WriteFile(pip, []byte(pipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Run from within dir so .mhl/state is created there.
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mhl", "--input", "issue_id=BUG-1"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"step: AuditWithSkill", "step: RefinementLoop", "executed 2 step(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// A completed pipeline clears its checkpoint.
	if _, err := os.Stat(filepath.Join(dir, ".mhl", "state", "AutoFixPipeline.json")); !os.IsNotExist(err) {
		t.Errorf("expected checkpoint cleared after successful run")
	}
}

func TestRunResumeFlagParsed(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mhl")
	if err := os.WriteFile(pip, []byte(pipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// With no prior checkpoint, --resume simply runs from the start.
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mhl", "--resume"}, &buf); err != nil {
		t.Fatalf("run --resume: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 2 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestRunMissingFile(t *testing.T) {
	var buf bytes.Buffer
	if err := cli.Run([]string{"run"}, &buf); err == nil {
		t.Fatal("expected usage error when no file given")
	}
}
