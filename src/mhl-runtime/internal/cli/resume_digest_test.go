package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const digestResumeV1 = `
pipeline Migrate {
    var stage = "start"
    step One { stage = "one-done" }
    step Two {
        stage = "two-start"
        fail("stop before finish")
    }
}
`

// Editing the pipeline's logic between a crashed run and a --resume must be
// refused: the checkpoint's variable state belongs to the old control flow.
// (P1-9 in PLANO.md.)
func TestRunResumeRefusesChangedDefinition(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(digestResumeV1), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var first bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &first); err == nil {
		t.Fatalf("expected the first run to fail at step Two:\n%s", first.String())
	}

	// Change step Two's body — same step names, different logic.
	edited := strings.Replace(digestResumeV1, `stage = "two-start"`, `stage = "two-start-EDITED"`, 1)
	if err := os.WriteFile("pipeline.mh", []byte(edited), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	var second bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh", "--resume"}, &second)
	if err == nil {
		t.Fatalf("expected --resume to be refused after the edit:\n%s", second.String())
	}
	if !strings.Contains(err.Error(), "different pipeline definition") {
		t.Fatalf("want a definition-mismatch error, got: %v", err)
	}

	// Restore the original definition — resume proceeds (and fails at Two again).
	if err := os.WriteFile("pipeline.mh", []byte(digestResumeV1), 0o644); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var third bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--resume"}, &third); err == nil {
		t.Fatalf("expected the restored resume to reach step Two and fail:\n%s", third.String())
	}
}

// --force downgrades the definition-mismatch to a warning and resumes anyway.
func TestRunResumeForceOverridesChangedDefinition(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(digestResumeV1), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var first bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &first); err == nil {
		t.Fatalf("expected the first run to fail at step Two:\n%s", first.String())
	}

	edited := strings.Replace(digestResumeV1, `stage = "two-start"`, `stage = "two-start-EDITED"`, 1)
	if err := os.WriteFile("pipeline.mh", []byte(edited), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	var second bytes.Buffer
	err := cli.Run([]string{"run", "pipeline.mh", "--resume", "--force"}, &second)
	// It still fails at step Two's fail() — but NOT with a definition-mismatch.
	if err == nil {
		t.Fatalf("expected step Two to still fail:\n%s", second.String())
	}
	if strings.Contains(err.Error(), "different pipeline definition") {
		t.Fatalf("--force should have overridden the digest check, got: %v", err)
	}
	if !strings.Contains(second.String(), "changed pipeline definition") {
		t.Fatalf("expected a --force warning in the output:\n%s", second.String())
	}
}

// --force without --resume is rejected.
func TestRunForceRequiresResume(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("pipeline.mh", []byte(digestResumeV1), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := cli.Run([]string{"run", "pipeline.mh", "--force"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "only applies with --resume") {
		t.Fatalf("want a --force/--resume error, got: %v", err)
	}
}
