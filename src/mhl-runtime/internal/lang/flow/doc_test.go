package flow

import "testing"

// A workflow's `///` block is its Description, and DocSpan covers the
// block's lines — from the first `///` to the end of the last line — so an
// editor can rewrite it; none leaves DocSpan nil.
func TestBuildDocSpan(t *testing.T) {
	src := "// file comment\n\n  /// Reviews a diff.\n  ///\n  /// Nothing is merged.  \nexport workflow Review {\n    step A { }\n}\nworkflow Plain { step A { } }\n"
	g := build(t, src)
	w := g.Workflows[0]
	if w.Description != "Reviews a diff.\n\nNothing is merged." {
		t.Errorf("Description = %q", w.Description)
	}
	if w.DocSpan == nil {
		t.Fatal("DocSpan = nil")
	}
	if got, want := slice(src, *w.DocSpan), "/// Reviews a diff.\n  ///\n  /// Nothing is merged."; got != want {
		t.Errorf("DocSpan text = %q, want %q", got, want)
	}
	if p := g.Workflows[1]; p.DocSpan != nil || p.Description != "" {
		t.Errorf("Plain: DocSpan = %v, Description = %q, want none", p.DocSpan, p.Description)
	}
}

// A merged partial takes its description from whichever fragment carries
// the `///` block, not only the graphed file's.
func TestBuildMergedDescriptionFromAnotherFragment(t *testing.T) {
	home := "import \"b.mh\"\npartial workflow W {\n    entry step A { }\n}"
	other := "/// The whole flow.\npartial workflow W {\n    step B { }\n}"
	w, err := BuildMerged([]Fragment{fragment(t, "", home), fragment(t, "/abs/b.mh", other)})
	if err != nil {
		t.Fatal(err)
	}
	if w.Description != "The whole flow." || w.DocSpan != nil {
		t.Errorf("Description = %q, DocSpan = %v; want the other fragment's doc, and no span in this file", w.Description, w.DocSpan)
	}
}
