package parser

import "testing"

// The `///` lines right above a pipeline/workflow or an alias — above its
// `export`/`internal`/`loop` prefixes too — are its Doc: a paragraph's lines
// joined with a space, an empty `///` line breaking paragraphs. A blank
// line, a plain `//` line or a `////` line ends the block.
func TestDeclDocsAttach(t *testing.T) {
	prog, err := Parse(`/// not attached: a blank line follows

/// Reviews a diff
/// and reports whether it can merge.
///
/// Nothing is merged.
export internal workflow Review { step A { } }
// plain comment
workflow Plain { step A { } }
//// banner
workflow Banner { step A { } }
/// Polls until done.
loop workflow Poll max 3 { step A { break } }
/// Review, for a release.
workflow Release = Review
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"Review": "Reviews a diff and reports whether it can merge.\n\nNothing is merged.",
		"Plain":  "",
		"Banner": "",
		"Poll":   "Polls until done.",
	}
	for _, d := range prog.Decls {
		switch {
		case d.Pipeline != nil:
			if got := d.Pipeline.Doc; got != want[d.Pipeline.Name] {
				t.Errorf("%s: Doc = %q, want %q", d.Pipeline.Name, got, want[d.Pipeline.Name])
			}
		case d.Alias != nil:
			if got := d.Alias.Description(); got != "Review, for a release." {
				t.Errorf("alias: Description() = %q", got)
			}
		}
	}
}
