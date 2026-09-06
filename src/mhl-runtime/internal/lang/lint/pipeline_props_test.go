package lint_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
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
    output: { x: x }
    var x = 0
    step S { x = 1 }
}
`)
	for _, f := range lint.File(main) {
		if strings.Contains(f.Message, "unknown property") {
			t.Fatalf("unexpected unknown-property finding: %+v", f)
		}
	}
}

// Every entry of the shared ast.PipelineBodyProperties allow-list must be
// accepted by lint (so the list stays the single source of truth — adding one
// there is enough, no lint edit needed).
func TestEveryDeclaredBodyPropertyIsAccepted(t *testing.T) {
	dir := t.TempDir()
	for _, p := range ast.PipelineBodyProperties {
		main := filepath.Join(dir, p.Name+".mh")
		write(t, main, fmt.Sprintf(`
loop workflow W {
    %s: { }
    step S { var x = 1 }
}
`, p.Name))
		for _, f := range lint.File(main) {
			if strings.Contains(f.Message, fmt.Sprintf("unknown property %q", p.Name)) {
				t.Errorf("lint rejects declared body property %q: %s", p.Name, f.Message)
			}
		}
	}
}
