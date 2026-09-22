package lsp

import (
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
