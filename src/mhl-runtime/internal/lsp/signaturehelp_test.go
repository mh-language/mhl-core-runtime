package lsp

import (
	"strings"
	"testing"
)

func TestCompletionCarriesSignatureDetail(t *testing.T) {
	text := "git."
	items := completionAt("main.mh", text, position{Line: 0, Character: len(text)})
	var diff *completionItem
	for i := range items {
		if items[i].Label == "diff" {
			diff = &items[i]
		}
	}
	if diff == nil {
		t.Fatalf("git.: no `diff` member offered")
	}
	if !strings.Contains(diff.Detail, "git.diff(target?: string, dir?: string)") {
		t.Errorf("diff.Detail = %q, want the real signature", diff.Detail)
	}
	if diff.Documentation == nil || !strings.Contains(diff.Documentation.Value, "diff") {
		t.Errorf("diff.Documentation = %+v, want a doc string", diff.Documentation)
	}
}

func TestSignatureHelpNativeOp(t *testing.T) {
	src, pos := posAtMarker(t, "pipeline P {\n step S {\n  var r = git.commit(\"msg\", §)\n }\n}\n")
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside git.commit(...)")
	}
	got := sh.Signatures[0].Label
	if !strings.Contains(got, "git.commit(message: string") {
		t.Errorf("label = %q, want the git.commit signature", got)
	}
	if sh.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1 (past the first comma)", sh.ActiveParameter)
	}
	if len(sh.Signatures[0].Parameters) != 3 {
		t.Errorf("Parameters = %+v, want 3 (message, paths, dir)", sh.Signatures[0].Parameters)
	}
}

func TestSignatureHelpFirstArgument(t *testing.T) {
	src, pos := posAtMarker(t, "pipeline P {\n step S {\n  log(time.parse(§))\n }\n}\n")
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside time.parse(...)")
	}
	if !strings.HasPrefix(sh.Signatures[0].Label, "time.parse(") {
		t.Errorf("label = %q", sh.Signatures[0].Label)
	}
	if sh.ActiveParameter != 0 {
		t.Errorf("ActiveParameter = %d, want 0", sh.ActiveParameter)
	}
}

// TestSignatureHelpActiveParameterFollowsNamedArgument is the regression
// test for the reported gap: mhl calls bind named arguments in any order,
// so skipping straight to a later optional parameter — `http.post(url:
// "", body: {}, tls: §)` — must highlight "tls" (its declared index, 10),
// not "query" (index 2, the raw top-level-comma count), which is what a
// caller sees if they've filled fewer arguments than tls's position
// implies. Before this fix, ActiveParameter followed the comma count
// alone, so skipping ahead like this always pointed at the wrong
// parameter's documentation.
func TestSignatureHelpActiveParameterFollowsNamedArgument(t *testing.T) {
	src, pos := posAtMarker(t, `pipeline P {
 step S {
  var r = http.post(url: "", body: {}, tls: §)
 }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside http.post(...)")
	}
	if sh.ActiveParameter != 10 {
		t.Errorf("ActiveParameter = %d, want 10 (\"tls\")", sh.ActiveParameter)
	}
	if sh.Signatures[0].Parameters[sh.ActiveParameter].Label != "tls" {
		t.Errorf("active parameter label = %q, want \"tls\"", sh.Signatures[0].Parameters[sh.ActiveParameter].Label)
	}

	// A positional (unnamed) argument in the same position must still fall
	// back to the plain comma count, unchanged from before this fix.
	srcPos, posPos := posAtMarker(t, `pipeline P {
 step S {
  var r = http.post("", {}, §)
 }
}
`)
	shPos := signatureHelpAt("main.mh", srcPos, posPos)
	if shPos == nil {
		t.Fatal("expected signature help inside http.post(...)")
	}
	if shPos.ActiveParameter != 2 {
		t.Errorf("positional ActiveParameter = %d, want 2 (\"query\")", shPos.ActiveParameter)
	}
}

// TestSignatureHelpHttpParamHasFocusedDocumentation is the regression test
// for the reported gap: http.post's dozen options are all packed into one
// signature-level paragraph, giving the editor nowhere to show a focused
// explanation for whichever one argument the cursor is actually on. Each
// ParameterInformation now carries its own Documentation (signatures.go's
// httpParamDocs), so e.g. landing on `text` explains that argument
// specifically instead of just repeating the whole call's summary.
func TestSignatureHelpHttpParamHasFocusedDocumentation(t *testing.T) {
	src, pos := posAtMarker(t, `pipeline P {
 step S {
  var r = http.post(url: "https://x", text: §)
 }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside http.post(...)")
	}
	params := sh.Signatures[0].Parameters
	var text *parameterInformation
	for i := range params {
		if params[i].Label == "text" {
			text = &params[i]
		}
	}
	if text == nil {
		t.Fatalf("http.post signature help has no \"text\" parameter")
	}
	if text.Documentation == nil {
		t.Fatal("expected per-parameter documentation on \"text\", got none")
	}
	if !strings.Contains(text.Documentation.Value, "Mutually exclusive with `body`/`form`") {
		t.Errorf("text.Documentation = %q, missing the mutual-exclusivity note", text.Documentation.Value)
	}
	// A parameter with no dedicated doc (there is none left undocumented in
	// httpParamDocs today, but the mechanism must degrade gracefully) still
	// falls back to nil rather than an empty non-nil MarkupContent.
	if sh.Signatures[0].Documentation == nil {
		t.Error("expected the overall call summary to still be present alongside per-parameter docs")
	}
}

func TestSignatureHelpBareAssertion(t *testing.T) {
	src, pos := posAtMarker(t, "test T {\n describe D {\n  are_equal(x, §)\n }\n}\n")
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside are_equal(...)")
	}
	if !strings.HasPrefix(sh.Signatures[0].Label, "are_equal(actual: any, expected: any)") {
		t.Errorf("label = %q", sh.Signatures[0].Label)
	}
	if sh.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1", sh.ActiveParameter)
	}
}

func TestSignatureHelpDeclaredMemory(t *testing.T) {
	src, pos := posAtMarker(t, "memory Store {\n type: \"kv\"\n store: \"memory\"\n}\npipeline P {\n step S {\n  Store.set(§)\n }\n}\n")
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Store.set(...)")
	}
	if !strings.HasPrefix(sh.Signatures[0].Label, "set(key: string, value: any)") {
		t.Errorf("label = %q", sh.Signatures[0].Label)
	}
}

func TestSignatureHelpDeclaredA2AExtension(t *testing.T) {
	src, pos := posAtMarker(t, "extension a2a Remote {\n url: \"http://x/a2a\"\n}\npipeline P {\n step S {\n  Remote.send(§)\n }\n}\n")
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Remote.send(...)")
	}
	if !strings.HasPrefix(sh.Signatures[0].Label, "send(message: string, context?: string)") {
		t.Errorf("label = %q", sh.Signatures[0].Label)
	}
}

func TestSignatureHelpOutsideAnyCallIsNil(t *testing.T) {
	src, pos := posAtMarker(t, "pipeline P {\n step S {\n  var x = 1 §\n }\n}\n")
	if sh := signatureHelpAt("main.mh", src, pos); sh != nil {
		t.Errorf("expected nil outside any call, got %+v", sh)
	}
}

func TestSignatureHelpUnknownCalleeIsNil(t *testing.T) {
	src, pos := posAtMarker(t, "pipeline P {\n step S {\n  SomeTool.whatever(§)\n }\n}\n")
	if sh := signatureHelpAt("main.mh", src, pos); sh != nil {
		t.Errorf("expected nil for an unknown callee, got %+v", sh)
	}
}

func TestEnclosingCallSkipsStringsAndComments(t *testing.T) {
	// The "(" and "," inside the string and the comment must not count.
	got, commas, _, ok := enclosingCall(`foo("a, (b)", bar, ` + "// ),\n" + `baz`)
	if !ok {
		t.Fatal("expected to be inside foo(...)")
	}
	if commas != 2 {
		t.Errorf("commas = %d, want 2", commas)
	}
	if got != 3 {
		t.Errorf("open index = %d, want 3", got)
	}
}
