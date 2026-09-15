package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestToolScopeBindingsAreChecked(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, `
tool T {
    const base = 2
    var offset = base + 1
    good() -> {
        offset = offset + 1
        return base
    }
    bad() -> { base = 3 }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, `cannot assign to constant "base"`) {
		t.Fatalf("expected constant reassignment finding, got %+v", findings)
	}
}
