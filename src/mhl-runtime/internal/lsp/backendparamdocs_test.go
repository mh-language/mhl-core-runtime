package lsp

import (
	"strings"
	"testing"
)

// TestSignatureHelpMemoryDocIsBackendSpecific is the regression test for
// the audited gap: memoryMethodSigs' Doc used to describe kv/json/
// append_log/jsonl all at once regardless of which backend the receiver
// actually declared. memorySig (declsig.go) now resolves the declaration's
// own `type:` and swaps in memoryMethodSigsByType's focused text.
func TestSignatureHelpMemoryDocIsBackendSpecific(t *testing.T) {
	t.Run("json backend explains :: navigation, not kv", func(t *testing.T) {
		src, pos := posAtMarker(t, `
memory Session {
    type: "json"
    path: ".mhl/session.json"
}
pipeline P {
    step S {
        var r = Session.get(§)
    }
}
`)
		sh := signatureHelpAt("main.mh", src, pos)
		if sh == nil {
			t.Fatal("expected signature help inside Session.get(...)")
		}
		doc := sh.Signatures[0].Documentation
		if doc == nil || !strings.Contains(doc.Value, "::") {
			t.Errorf("doc = %v, expected the json backend's :: navigation note", doc)
		}
	})

	t.Run("kv backend has no :: navigation note", func(t *testing.T) {
		src, pos := posAtMarker(t, `
memory Cache {
    type: "kv"
    store: "memory"
}
pipeline P {
    step S {
        var r = Cache.get(§)
    }
}
`)
		sh := signatureHelpAt("main.mh", src, pos)
		if sh == nil {
			t.Fatal("expected signature help inside Cache.get(...)")
		}
		doc := sh.Signatures[0].Documentation
		if doc != nil && strings.Contains(doc.Value, "::") {
			t.Errorf("doc = %v, kv has no :: navigation but the json wording leaked in", doc)
		}
	})
}

// TestSignatureHelpHtmlAttrsParamExplainsClassMatching is the regression
// test for the audited gap: get_element's class-vs-everything-else
// matching rule was buried mid-paragraph in the whole call's Doc. Now
// landing on the `attrs` argument specifically explains it.
func TestSignatureHelpHtmlAttrsParamExplainsClassMatching(t *testing.T) {
	src, pos := posAtMarker(t, `
pipeline P {
    step S {
        var el = html.get_element(node, attrs: §)
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside html.get_element(...)")
	}
	var attrs *parameterInformation
	for i := range sh.Signatures[0].Parameters {
		if sh.Signatures[0].Parameters[i].Label == "attrs" {
			attrs = &sh.Signatures[0].Parameters[i]
		}
	}
	if attrs == nil {
		t.Fatal("no \"attrs\" parameter in html.get_element's signature")
	}
	if attrs.Documentation == nil || !strings.Contains(attrs.Documentation.Value, "space-separated") {
		t.Errorf("attrs.Documentation = %v, missing the class-matching rule", attrs.Documentation)
	}
}

// TestSignatureHelpMcpCallArgumentsParamHasItsOwnDoc is the regression test
// for the audited gap: ParamSpec had no Documentation field at all, so an
// mcp/a2a extension's own params never got focused per-parameter help the
// way http.* has since the first round of this audit.
func TestSignatureHelpMcpCallArgumentsParamHasItsOwnDoc(t *testing.T) {
	src, pos := posAtMarker(t, `
extension mcp Server {
    command: "my-mcp-server"
}
pipeline P {
    step S {
        var r = Server.call(tool: "x", arguments: §)
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Server.call(...)")
	}
	var args *parameterInformation
	for i := range sh.Signatures[0].Parameters {
		if sh.Signatures[0].Parameters[i].Label == "arguments" {
			args = &sh.Signatures[0].Parameters[i]
		}
	}
	if args == nil {
		t.Fatal("no \"arguments\" parameter in Server.call's signature")
	}
	if args.Documentation == nil || !strings.Contains(args.Documentation.Value, "inputSchema") {
		t.Errorf("arguments.Documentation = %v, missing the per-parameter explanation", args.Documentation)
	}
}
