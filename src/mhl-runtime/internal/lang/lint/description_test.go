package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A `///` description is clean, on a workflow and on an alias.
func TestDocCommentDescriptionLintsClean(t *testing.T) {
	findings := lintSrc(t, `
/// Reviews a diff and reports whether it can merge.
workflow Review { step A { log("a") } }
/// Review, for a release.
workflow Release = Review
`)
	if len(findings) != 0 {
		t.Fatalf("want no findings, got %v", messages(findings))
	}
}

// `description: "..."` still works, with a warning showing the `///` line
// to write instead; on an alias too.
func TestDescriptionPropertyIsDeprecated(t *testing.T) {
	findings := lintSrc(t, `
workflow Review {
    description: "Reviews a diff."
    step A { log("a") }
}
workflow Release = Review { description: "Review, for a release." }
`)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %v", messages(findings))
	}
	for i, want := range []string{
		"`/// Reviews a diff.` above `workflow Review`",
		"`/// Review, for a release.` above `workflow Release = Review`",
	} {
		f := findings[i]
		if f.Severity != lint.SeverityWarning || !strings.Contains(f.Message, "is deprecated") || !strings.Contains(f.Message, want) {
			t.Errorf("finding %d = %+v, want a deprecation warning suggesting %s", i, f, want)
		}
	}
}

// Both spellings at once is an error: serve publishes one description.
func TestDescriptionDeclaredTwice(t *testing.T) {
	findings := lintSrc(t, `
/// From the doc.
workflow Review {
    description: "From the property."
    step A { log("a") }
}
`)
	if len(findings) != 1 || findings[0].Severity == lint.SeverityWarning || !strings.Contains(findings[0].Message, "declared twice") {
		t.Fatalf("want one 'declared twice' error, got %+v", findings)
	}
}

// A partial workflow's description is written above one fragment; a second
// documented fragment is an error, like a second `max`.
func TestPartialDescriptionOnOneFragment(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
/// The gate.
partial workflow Discovery {
    step Gate { log("g") }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

/// Discovers a feature.
partial workflow Discovery {
    entry step Dispatch { goto Gate }
}
`)
	msgs := strings.Join(messages(lint.File(main)), "\n")
	if !strings.Contains(msgs, "written above more than one fragment") {
		t.Fatalf("want a 'more than one fragment' error, got:\n%s", msgs)
	}
}
