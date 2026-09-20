package parser

import "testing"

// TestParsePartialWorkflowAndEntryStep covers the grammar added for
// splitting a pipeline/workflow across files: a leading `partial` keyword
// on the declaration, an `entry` keyword on the step that should run first
// once fragments are merged, and the bare `import "path"` (no `{ }` list)
// form that pulls a fragment file in wholesale.
func TestParsePartialWorkflowAndEntryStep(t *testing.T) {
	src := `
import "fragment.mh"
import { Foo } from "other.mh"

partial workflow Discovery {
    entry step Dispatch {
        goto Gate
    }
    step Gate {}
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(prog.Decls) != 3 {
		t.Fatalf("expected 3 decls, got %d", len(prog.Decls))
	}

	whole := prog.Decls[0].Import
	if whole == nil || !whole.IsWhole() {
		t.Fatalf("expected decl 0 to be a whole-file import")
	}
	if whole.Path != "fragment.mh" {
		t.Fatalf("unexpected path: %q", whole.Path)
	}
	if len(whole.Names()) != 0 {
		t.Fatalf("expected no names on a whole-file import, got %v", whole.Names())
	}

	named := prog.Decls[1].Import
	if named == nil || named.IsWhole() {
		t.Fatalf("expected decl 1 to be a named import")
	}

	p := prog.Decls[2].Pipeline
	if p == nil || !p.Partial {
		t.Fatalf("expected decl 2 to be a partial pipeline")
	}
	if !p.IsWorkflow() {
		t.Fatalf("expected workflow kind")
	}
	if p.Body[0].Step == nil || !p.Body[0].Step.Entry {
		t.Fatalf("expected Dispatch to be marked entry")
	}
	if p.Body[1].Step == nil || p.Body[1].Step.Entry {
		t.Fatalf("expected Gate to not be marked entry")
	}
}

// TestParseNonPartialPipelineRejectsPartialKeywordAbsence just documents
// that a plain declaration (no `partial`) still parses exactly as before —
// Partial/Entry both default to false with nothing written.
func TestParseNonPartialPipelineRejectsPartialKeywordAbsence(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    step S {}
}
`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p := prog.Decls[0].Pipeline
	if p.Partial {
		t.Fatalf("expected Partial to default to false")
	}
	if p.Body[0].Step.Entry {
		t.Fatalf("expected Entry to default to false")
	}
}
