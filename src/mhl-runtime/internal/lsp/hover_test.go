package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHoverOnNativeMethodShowsSignatureAndDoc is the regression test for
// the reported gap: signature help only fires inside an open call, so
// browsing/reading code (not writing it) got zero explanation for
// `os.user()` or any other native op. hoverAt reuses signatureForMethod, so
// hovering the call site shows exactly what signature help would.
func TestHoverOnNativeMethodShowsSignatureAndDoc(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    step S {
        log.info(os.us§er())
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over os.user")
	}
	if !strings.Contains(h.Contents.Value, "os.user() -> string") {
		t.Errorf("hover = %q, missing the signature", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "OS account name") {
		t.Errorf("hover = %q, missing the doc", h.Contents.Value)
	}
}

// TestHoverOnNativeNamespaceListsMethods covers hovering the bare namespace
// itself (no member chosen yet), e.g. "http" in "http.post(...)".
func TestHoverOnNativeNamespaceListsMethods(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    step S {
        var r = ht§tp.post(url: "https://x")
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over http")
	}
	if !strings.Contains(h.Contents.Value, "`post`") {
		t.Errorf("hover = %q, missing the post method in the list", h.Contents.Value)
	}
}

// TestHoverOnToolNameShowsMethodsAndDocComment proves hovering a declared
// tool's own name (not a member access) shows its kind, its leading "//"
// doc comment, and its method list.
func TestHoverOnToolNameShowsMethodsAndDocComment(t *testing.T) {
	src, pos := posAtMarker(t, `
// Wraps go test/build for this repo's checkout.
tool Project {
    read(path: string): string -> fs.read(path)
}

pipeline P {
    step S {
        var t = Pro§ject.read("x")
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over Project")
	}
	if !strings.Contains(h.Contents.Value, "tool Project") {
		t.Errorf("hover = %q, missing the kind+name header", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "Wraps go test/build for this repo's checkout.") {
		t.Errorf("hover = %q, missing the leading doc comment", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "`read`") {
		t.Errorf("hover = %q, missing the method list", h.Contents.Value)
	}
}

// TestHoverOnToolMethodShowsItsOwnSignature covers the "receiver.method"
// hover path, sharing toolMethodSig with signature help.
func TestHoverOnToolMethodShowsItsOwnSignature(t *testing.T) {
	src, pos := posAtMarker(t, `
tool Project {
    read(path: string): string -> fs.read(path)
}

pipeline P {
    step S {
        var t = Project.re§ad("x")
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over Project.read")
	}
	if !strings.Contains(h.Contents.Value, "read(path: string): string") {
		t.Errorf("hover = %q, missing the method's own signature", h.Contents.Value)
	}
}

// TestHoverOnPromptNameShowsCallSignature covers hovering a declared
// prompt's own name.
func TestHoverOnPromptNameShowsCallSignature(t *testing.T) {
	src, pos := posAtMarker(t, `
prompt Summarize(text: string, max_words: number = 100) {
    "Summarize in ${max_words} words: ${text}"
}

agent A {
    command: "echo"
}

pipeline P {
    step S {
        var r = A.run(prompt: Summ§arize(text: "x"))
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over Summarize")
	}
	if !strings.Contains(h.Contents.Value, "prompt Summarize") {
		t.Errorf("hover = %q, missing the kind+name header", h.Contents.Value)
	}
	if !strings.Contains(h.Contents.Value, "Summarize(text: string, max_words?: number)") {
		t.Errorf("hover = %q, missing the call signature", h.Contents.Value)
	}
}

// TestHoverOnGlobalBuiltinShowsSignature covers a bare global like env(),
// which has no receiver and isn't a declared symbol.
func TestHoverOnGlobalBuiltinShowsSignature(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    step S {
        log.info(e§nv("TOKEN"))
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over env")
	}
	if !strings.Contains(h.Contents.Value, "env(name: string, default?: string)") {
		t.Errorf("hover = %q, missing env's signature", h.Contents.Value)
	}
}

// TestHoverOutsideAnyResolvableTokenIsNil proves an ordinary local variable
// (nothing static to say about it) yields no hover, rather than something
// misleading.
func TestHoverOutsideAnyResolvableTokenIsNil(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    step S {
        var my_local_thing = 1
        log.info(my_local_th§ing)
    }
}
`)
	if h := hoverAt("main.mh", src, pos); h != nil {
		t.Errorf("expected nil hover for a plain local variable, got %q", h.Contents.Value)
	}
}

// Hovering a workflow input — where it is read — shows its `///` doc and
// when the run asks for it; one declared in another fragment of a partial
// workflow is found there.
func TestHoverOnInputShowsItsDoc(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    /// The folder to work in.
    input project_dir: string
    step S {
        log(project_§dir)
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil || !strings.Contains(h.Contents.Value, "input project_dir: string") || !strings.Contains(h.Contents.Value, "The folder to work in.") || !strings.Contains(h.Contents.Value, "when the run of `W` starts") {
		t.Fatalf("hover = %v", h)
	}

	dir := t.TempDir()
	writeFile := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, dir+"/main.mh", "import \"story/plan.mh\"\npartial workflow W {\n    /// Who the story is for.\n    input who: string\n    entry step A { goto Plan }\n}\n")
	frag, pos := posAtMarker(t, "partial workflow W stage Plan {\n    /// The plan to pick.\n    input plan: string\n    step B { log(w§ho, plan) }\n}\n")
	writeFile(t, dir+"/story/plan.mh", frag)
	h = hoverAt(dir+"/story/plan.mh", frag, pos)
	if h == nil || !strings.Contains(h.Contents.Value, "Who the story is for.") {
		t.Fatalf("cross-fragment hover = %v", h)
	}
}

// TestHoverOnVariantPromptListsVariants covers a prompt with `for` arms: the
// hover lists every arm an agent can pick.
func TestHoverOnVariantPromptListsVariants(t *testing.T) {
	src, pos := posAtMarker(t, `
prompt Review(file: string) {
    default         { "Review ${file}." }
    for "claude/*"  { "<task>${file}</task>" }
    for "cli/codex" { "codex ${file}" }
}

pipeline P {
    step S {
        var r = Rev§iew(file: "x")
    }
}
`)
	h := hoverAt("main.mh", src, pos)
	if h == nil {
		t.Fatal("expected hover over Review")
	}
	if want := "Variants: `default` · `for \"claude/*\"` · `for \"cli/codex\"`"; !strings.Contains(h.Contents.Value, want) {
		t.Errorf("hover = %q, missing %q", h.Contents.Value, want)
	}
}
