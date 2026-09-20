package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGotoTargetCompletionOffersOwnSteps proves `goto <TAB>` inside a
// workflow offers every step it declares, including a `parallel` group's
// own branch steps — and that it does NOT fire for `goto match`'s subject
// expression, which isn't a step name position at all.
func TestGotoTargetCompletionOffersOwnSteps(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    step A { goto §
    }
    step B { log("b") }
    parallel Fanout {
        step C { log("c") }
        step D { log("d") }
    }
}
`)
	items := completionAt("/proj/main.mh", src, pos)
	for _, want := range []string{"A", "B", "C", "D"} {
		if !hasLabel(items, want) {
			t.Errorf("missing step %q in goto-target completion, got: %+v", want, items)
		}
	}
}

// TestGotoMatchSubjectDoesNotOfferStepNames proves the exclusion: `goto
// match <subject>` isn't a step-name position, so it must not spuriously
// list step names as if it were (that would suggest invalid code — a step
// name isn't a legal `goto match` subject expression).
func TestGotoMatchSubjectDoesNotOfferStepNames(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    step A { goto match §
    }
    step B { log("b") }
}
`)
	items := completionAt("/proj/main.mh", src, pos)
	if hasLabel(items, "B") {
		t.Error("goto match's subject position must not offer step names")
	}
}

// TestGotoTargetCompletionReachesPartialSiblingSteps proves the whole point:
// a `goto` in one `partial` fragment offers step names declared in a
// sibling fragment file too, mirroring self-completion's own reach for
// input/var/mem.
func TestGotoTargetCompletionReachesPartialSiblingSteps(t *testing.T) {
	dir := t.TempDir()
	sibling := filepath.Join(dir, "sibling.mh")
	if err := os.WriteFile(sibling, []byte("partial workflow Discovery {\n    step BriefGenerate { log(\"brief\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mh")
	src, pos := posAtMarker(t, `partial workflow Discovery {
    entry step Dispatch {
        goto §
    }
}
`)
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	items := completionAt(main, src, pos)
	if !hasLabel(items, "BriefGenerate") {
		t.Errorf("expected the sibling fragment's step BriefGenerate in goto-target completion, got: %+v", items)
	}
	if !hasLabel(items, "Dispatch") {
		t.Errorf("expected the current fragment's own step Dispatch too, got: %+v", items)
	}
}

// TestDefinitionGotoTargetSameFile proves "go to definition" on a `goto
// <Step>` target resolves to that step's own declaration — a gap plain
// findDeclaration can't close, since a step is never a top-level name.
func TestDefinitionGotoTargetSameFile(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    step A { goto B§ }
    step B { log("b") }
}
`)
	locs := definitionAt("/proj/main.mh", src, pos, nil)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %d: %+v", len(locs), locs)
	}
	if locs[0].Range.Start.Line != 3 {
		t.Errorf("start line = %d, want 3 (the `step B` declaration)", locs[0].Range.Start.Line)
	}
}

// TestDefinitionGotoTargetPartialSibling proves the cross-file reach: a
// `goto` in one partial fragment resolves to a step declared in a sibling
// fragment file.
func TestDefinitionGotoTargetPartialSibling(t *testing.T) {
	dir := t.TempDir()
	sibling := filepath.Join(dir, "sibling.mh")
	if err := os.WriteFile(sibling, []byte("partial workflow Discovery {\n    step BriefGenerate { log(\"brief\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mh")
	src, pos := posAtMarker(t, `partial workflow Discovery {
    entry step Dispatch {
        goto BriefGe§nerate
    }
}
`)
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := definitionAt(main, src, pos, nil)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %d: %+v", len(locs), locs)
	}
	if locs[0].URI != pathToURI(sibling) {
		t.Errorf("uri = %q, want %q (the sibling fragment)", locs[0].URI, pathToURI(sibling))
	}
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("start line = %d, want 1 (the `step BriefGenerate` declaration)", locs[0].Range.Start.Line)
	}
}

// TestDefinitionGotoMatchArmDoesNotMisfire proves the exclusion carries over
// to definitionAt too: the cursor on a `goto match` subject identifier
// resolves through the ordinary top-level path (or not at all), never
// through findStepDeclaration.
func TestDefinitionGotoMatchArmDoesNotMisfire(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    var artifact = "x"
    step A { goto match art§ifact { _ -> B } }
    step B { log("b") }
}
`)
	locs := definitionAt("/proj/main.mh", src, pos, nil)
	if len(locs) != 0 {
		t.Errorf("want no definition for a goto match subject (not a step name), got: %+v", locs)
	}
}
