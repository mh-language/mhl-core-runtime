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
	got, commas, ok := enclosingCall(`foo("a, (b)", bar, ` + "// ),\n" + `baz`)
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
