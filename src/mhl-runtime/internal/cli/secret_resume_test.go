package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const secretResumeFile = `
pipeline SecretResume {
    var token = env("MHL_RESUME_API_TOKEN")
    var seen = ""

    step One {
        seen = token
        log("one done")
    }

    step Two {
        if (token == "resume-secret-abc123") {
            log("token-ok")
        } else {
            log("token-bad")
        }
        fail("stop before done")
    }
}
`

// A credential read via env() into a workflow variable must survive a
// fresh-process --resume: the checkpoint stores the env() reference (never
// the resolved value) and Load re-resolves it, so the resumed step sees the
// live secret rather than a dead "[REDACTED]" mask. (P0-4 in PLANO.md.)
func TestRunResumeRehydratesCredentialFromReference(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv("MHL_RESUME_API_TOKEN", "resume-secret-abc123")
	if err := os.WriteFile("pipeline.mh", []byte(secretResumeFile), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var first bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh"}, &first); err == nil {
		t.Fatalf("expected the first run to fail at step Two:\n%s", first.String())
	}

	// The persisted checkpoint must not contain the resolved secret.
	cpPath := filepath.Join(dir, ".mhl", "state", "SecretResume.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		// Session-scoped layout: fall back to a search under .mhl/state.
		data = nil
		_ = filepath.WalkDir(filepath.Join(dir, ".mhl", "state"), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, "SecretResume.json") {
				b, _ := os.ReadFile(p)
				data = append(data, b...)
			}
			return nil
		})
	}
	if len(data) == 0 {
		t.Fatalf("no checkpoint file written under %s", filepath.Join(dir, ".mhl", "state"))
	}
	if strings.Contains(string(data), "resume-secret-abc123") {
		t.Fatalf("checkpoint persisted the resolved secret:\n%s", data)
	}
	if !strings.Contains(string(data), "MHL_RESUME_API_TOKEN") {
		t.Fatalf("checkpoint did not store the credential reference:\n%s", data)
	}

	var second bytes.Buffer
	if err := cli.Run([]string{"run", "pipeline.mh", "--resume"}, &second); err == nil {
		t.Fatalf("expected the resumed run to fail at step Two again:\n%s", second.String())
	}
	out := second.String()
	if !strings.Contains(out, "token-ok") || strings.Contains(out, "token-bad") {
		t.Fatalf("resumed step did not see the rehydrated secret:\n%s", out)
	}
}
