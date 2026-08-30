package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A bare property in a pipeline/workflow body that nothing reads is a
// finding — a typo like `checkpont:` or a docs-only field.
func TestUnknownPipelinePropertyIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow W {
    checkpont: { enabled: true }
    step S { var x = 1 }
}
`)
	if !hasMessage(lint.File(main), `unknown property "checkpont"`) {
		t.Fatalf("expected the misspelled `checkpont` to be flagged")
	}
}

// `description:` and the runtime-read config blocks are all accepted.
func TestKnownPipelinePropertiesAreClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
loop workflow W {
    description: "A useful workflow."
    checkpoint: { enabled: true, strategy: "per_step", ttl: 7d }
    spawn: { max_concurrency: 2 }
    repeat: { max_iterations: 3 }
    context: { source: "latest" }
    step S { var x = 1 }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "unknown property") {
			t.Fatalf("unexpected unknown-property finding: %+v", f)
		}
	}
}
