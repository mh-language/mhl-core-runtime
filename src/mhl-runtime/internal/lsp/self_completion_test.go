package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSelfCompletionInsideToolOffersSiblingMethods is the pre-existing gap
// fixed alongside the pipeline case: `self.` inside a tool method never
// offered any completion at all before, even though self.method() itself
// has always worked at runtime.
func TestSelfCompletionInsideToolOffersSiblingMethods(t *testing.T) {
	src, pos := posAtMarker(t, `tool Helper {
    ping() -> "pong"
    render(x: string): string -> {
        return self.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	if !hasLabel(items, "ping") {
		t.Errorf("expected self. inside a tool method to offer sibling method %q, got %+v", "ping", items)
	}
	if !hasLabel(items, "render") {
		t.Errorf("expected self. to offer %q (itself, recursion is legal), got %+v", "render", items)
	}
}

// TestSelfCompletionInsideNonPartialPipelineOffersOwnScope: the ordinary,
// single-file case — self. inside a step offers that pipeline's own
// declared input/var/mem, no cross-file scan needed.
func TestSelfCompletionInsideNonPartialPipelineOffersOwnScope(t *testing.T) {
	src, pos := posAtMarker(t, `workflow Demo {
    input name: string
    var greeting = ""
    mem attempts = 0
    step Build {
        self.§
    }
}
`)
	items := completionAt("main.mh", src, pos)
	for _, want := range []string{"name", "greeting", "attempts"} {
		if !hasLabel(items, want) {
			t.Errorf("expected self. to offer %q, got %+v", want, items)
		}
	}
}

// TestSelfCompletionAcrossPartialFragments is the actual point of the
// feature: editing one fragment of a `partial` pipeline, self. must surface
// input/var/mem declared in EVERY sibling fragment, not just this file's
// own — the discoverability gap reported against the real discovery.mh
// split (a dev has to read every fragment file by hand today to learn
// current_artifact/pending_data/... exist).
func TestSelfCompletionAcrossPartialFragments(t *testing.T) {
	dir := t.TempDir()
	fragment := `
partial workflow Discovery {
    var pending_data = null
    step BriefGenerate {
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.mh"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}
	mainSrc, pos := posAtMarker(t, `import "fragment.mh"

partial workflow Discovery {
    input project_id: string
    entry step Dispatch {
        self.§
    }
}
`)
	mainPath := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	items := completionAt(mainPath, mainSrc, pos)
	if !hasLabel(items, "project_id") {
		t.Errorf("expected self. to offer this file's own input %q, got %+v", "project_id", items)
	}
	if !hasLabel(items, "pending_data") {
		t.Errorf("expected self. to offer the sibling fragment's var %q, got %+v", "pending_data", items)
	}
}

// TestSelfCompletionIgnoresUnrelatedSiblingDeclarations: a sibling .mh file
// declaring a *different* partial pipeline (different name, or the same
// name but a plain non-partial one, or a different kind) must not leak its
// vars into self. completion — only a true fragment of the same partial
// declaration counts.
func TestSelfCompletionIgnoresUnrelatedSiblingDeclarations(t *testing.T) {
	dir := t.TempDir()
	unrelated := `
partial workflow OtherPipeline {
    var should_not_appear = null
    step X {
    }
}

workflow NotEvenPartial {
    var also_should_not_appear = null
    step Y {
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "unrelated.mh"), []byte(unrelated), 0o644); err != nil {
		t.Fatal(err)
	}
	mainSrc, pos := posAtMarker(t, `import "unrelated.mh"

partial workflow Discovery {
    var real_var = null
    entry step Dispatch {
        self.§
    }
}
`)
	mainPath := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	items := completionAt(mainPath, mainSrc, pos)
	if !hasLabel(items, "real_var") {
		t.Errorf("expected self. to offer %q, got %+v", "real_var", items)
	}
	if hasLabel(items, "should_not_appear") || hasLabel(items, "also_should_not_appear") {
		t.Errorf("expected self. to NOT leak an unrelated sibling declaration's vars, got %+v", items)
	}
}
