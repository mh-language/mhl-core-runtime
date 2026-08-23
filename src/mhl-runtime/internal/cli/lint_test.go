package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

func TestLintCommandReportsFindings(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
pipeline P {
    step S {
        var response = Ghost.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"lint", dir}, &buf)
	if err == nil {
		t.Fatal("expected a non-nil error when findings are reported")
	}
	out := buf.String()
	if !strings.Contains(out, `agent "Ghost" not found`) {
		t.Errorf("output missing finding: %s", out)
	}
	if !strings.Contains(out, "1 problem(s) found") {
		t.Errorf("output missing summary: %s", out)
	}
}

func TestLintCommandCleanProject(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent Reviewer {
    command: "bash"
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"lint", dir}, &buf); err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !strings.Contains(buf.String(), "No problems found.") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestLintDefaultDir(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
agent Reviewer {
    command: "bash"
}

pipeline P {
    step S {
        var response = Reviewer.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"lint"}, &buf); err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !strings.Contains(buf.String(), "No problems found.") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestLintUnknownDir(t *testing.T) {
	var buf bytes.Buffer
	err := cli.Run([]string{"lint", filepath.Join(t.TempDir(), "does-not-exist")}, &buf)
	if err == nil {
		t.Fatal("expected an error for a nonexistent directory")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no findings printed, got: %s", buf.String())
	}
}
