package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

const passingTestFile = `
test CodeAuditPipelineTest {
    describe conditional_statements {
        are_equal("a", "a")
        is_true(true)
        is_false(false)
        is_null(null)
        not_null("not null")
        not_equal("a", "b")
        greater_than(5, 3)
        less_than(3, 5)
        greater_than_or_equal(5, 5)
        less_than_or_equal(3, 3)
        are_not_equal(1, 2)
        includes([1, 2, 3], 2)
        incomplete("pending case")
    }
}
`

// mhl test runs every assertion in every describe block and reports a
// summary; an all-passing suite (plus one incomplete()) exits cleanly.
func TestRunTestsAllPass(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "suite.mh")
	if err := os.WriteFile(file, []byte(passingTestFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"test", file}, &buf); err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"test CodeAuditPipelineTest",
		"describe conditional_statements",
		`PASS are_equal("a", "a")`,
		"SKIP pending case",
		"12 passed, 0 failed, 1 incomplete",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

const failingTestFile = `
test FailDemo {
    describe checks {
        are_equal(1, 2)
        is_true(false)
    }
}
`

// A failed assertion is reported as FAIL with a reason, and mhl test exits
// with a non-nil error so the command line surfaces a non-zero exit code.
func TestRunTestsReportsFailure(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "suite.mh")
	if err := os.WriteFile(file, []byte(failingTestFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"test", file}, &buf)
	if err == nil {
		t.Fatalf("expected an error for a failing assertion")
	}
	out := buf.String()
	for _, want := range []string{
		"FAIL are_equal(1, 2) — expected 1 to equal 2",
		"FAIL is_true(false) — expected true, got false",
		"0 passed, 2 failed, 0 incomplete",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunTestsNoTestBlocks(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "empty.mh")
	if err := os.WriteFile(file, []byte("pipeline P { step S { var x = 1 } }"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"test", file}, &buf); err == nil {
		t.Fatalf("expected an error when no test blocks are declared")
	}
}

// mhl test <dir> recursively finds every .mh file, runs the test blocks in
// each, and aggregates one summary across all of them; files with no test
// blocks (e.g. a pipeline definition) are skipped rather than erroring out
// the whole run.
func TestRunTestsOnDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mh"), []byte(passingTestFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.mh"), []byte(failingTestFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "not_a_test.mh"), []byte("pipeline P { step S { var x = 1 } }"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"test", dir}, &buf)
	if err == nil {
		t.Fatalf("expected an error since one of the suites has a failing assertion")
	}
	out := buf.String()
	for _, want := range []string{
		"test CodeAuditPipelineTest",
		"test FailDemo",
		"FAIL are_equal(1, 2) — expected 1 to equal 2",
		"12 passed, 2 failed, 1 incomplete",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A directory whose .mh files declare no test blocks at all is an error,
// same as passing a single such file.
func TestRunTestsOnDirectoryNoTestBlocks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.mh"), []byte("pipeline P { step S { var x = 1 } }"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"test", dir}, &buf); err == nil {
		t.Fatalf("expected an error when no test blocks are declared under the directory")
	}
}
