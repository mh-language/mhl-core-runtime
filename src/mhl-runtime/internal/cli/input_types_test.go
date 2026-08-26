package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const typedInputPipelineFile = `
pipeline TypedInput {
    input count: number

    step Double {
        var doubled = count * 2
        log("doubled=${doubled}")
    }
}
`

// A `--input` value for a declared `input name: number` must be coerced to
// mhl's numeric runtime type (float64), not left as the raw CLI string —
// otherwise arithmetic on it (evalMul requiring float64 operands) would fail
// deep inside the first step that uses it, rather than at the boundary.
func TestRunInputCoercesToDeclaredNumberType(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(typedInputPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--input", "count=5"}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "doubled=10") {
		t.Errorf("expected numeric coercion (doubled=10), got:\n%s", out)
	}
}

// An --input value that can't be coerced to its declared type must fail
// before any step runs, with a clear error naming the input and the value —
// not as an opaque failure deep inside whatever expression first uses it.
func TestRunInputInvalidNumberFailsFast(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(typedInputPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh", "--input", "count=abc"}, &buf)
	if err == nil {
		t.Fatalf("expected an error for a non-numeric --input value, got nil")
	}
	if !strings.Contains(err.Error(), `"count"`) || !strings.Contains(err.Error(), "not a valid number") {
		t.Errorf("unexpected error message: %v", err)
	}
	if strings.Contains(buf.String(), "step:") {
		t.Errorf("expected failure before any step ran, got output:\n%s", buf.String())
	}
}

const typedArrayInputPipelineFile = `
pipeline TypedArrayInput {
    input tags: string[]

    step Show {
        log("count=${tags.size()}")
    }
}
`

// --input for a declared `input name: string[]` must be deep-validated
// element-by-element, not just "is this roughly a JSON array" — a shaped
// array type gets the same recursive Check every other typed boundary does.
func TestRunInputArrayElementTypeCoerced(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(typedArrayInputPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--input", `tags=["a","b","c"]`}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "count=3") {
		t.Errorf("expected count=3, got:\n%s", out)
	}
}

// A wrong-element-type --input array must fail fast, before any step runs —
// same fail-fast guarantee TestRunInputInvalidNumberFailsFast locks in for a
// flat `number` input, now for a shaped `string[]` one.
func TestRunInputArrayElementTypeFailsFast(t *testing.T) {
	dir := t.TempDir()
	pip := filepath.Join(dir, "pipeline.mh")
	if err := os.WriteFile(pip, []byte(typedArrayInputPipelineFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh", "--input", `tags=["a",2]`}, &buf)
	if err == nil {
		t.Fatalf("expected an error for a wrong-element-type --input array, got nil")
	}
	if !strings.Contains(err.Error(), `"tags"[1]`) || !strings.Contains(err.Error(), "must be string, got number") {
		t.Errorf("unexpected error message: %v", err)
	}
	if strings.Contains(buf.String(), "step:") {
		t.Errorf("expected failure before any step ran, got output:\n%s", buf.String())
	}
}
