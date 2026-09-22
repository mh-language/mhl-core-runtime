package lsp

import "testing"

// TestSignatureHelpToolMethodResolvesRealParams is the regression test for
// the reported gap: signatureForMethod always returned (sig{}, false) for a
// user-declared tool method, so `Project.read(§)` got no signature help at
// all — not even the method's own parameter names. toolMethodSig
// (declsig.go) now resolves it from the tool's real declaration.
func TestSignatureHelpToolMethodResolvesRealParams(t *testing.T) {
	src, pos := posAtMarker(t, `
tool Project {
    read(path: string): string -> fs.read(path)

    test(package: string, timeout: duration = 30s): {stdout: string} -> cmd.exec([package])
}

pipeline P {
    step S {
        var out = Project.test(§)
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Project.test(...)")
	}
	if sh.Signatures[0].Label != `test(package: string, timeout?: duration): { stdout: string }` {
		t.Errorf("label = %q", sh.Signatures[0].Label)
	}
	if len(sh.Signatures[0].Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(sh.Signatures[0].Parameters))
	}
	if sh.Signatures[0].Parameters[0].Label != "package" || sh.Signatures[0].Parameters[1].Label != "timeout" {
		t.Errorf("parameters = %+v", sh.Signatures[0].Parameters)
	}
}

// TestSignatureHelpToolMethodActiveParameterFollowsName proves the same
// named-argument-aware active-parameter tracking (signaturehelp.go) works
// for a tool method too, not just the native http.* family.
func TestSignatureHelpToolMethodActiveParameterFollowsName(t *testing.T) {
	src, pos := posAtMarker(t, `
tool Project {
    test(package: string, timeout: duration = 30s): {stdout: string} -> cmd.exec([package])
}

pipeline P {
    step S {
        var out = Project.test(timeout: §)
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Project.test(...)")
	}
	if sh.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1 (\"timeout\")", sh.ActiveParameter)
	}
}

// TestSignatureHelpPromptResolvesRealParams covers a bare call to a
// declared `prompt Name(...)` — signatureForBareCall previously only knew
// about globals/assertions, so a prompt call got no signature help either.
func TestSignatureHelpPromptResolvesRealParams(t *testing.T) {
	src, pos := posAtMarker(t, `
prompt Summarize(text: string, max_words: number = 100) {
    "Summarize in ${max_words} words: ${text}"
}

agent A {
    command: "echo"
}

pipeline P {
    step S {
        var r = A.run(prompt: Summarize(§))
    }
}
`)
	sh := signatureHelpAt("main.mh", src, pos)
	if sh == nil {
		t.Fatal("expected signature help inside Summarize(...)")
	}
	if sh.Signatures[0].Label != "Summarize(text: string, max_words?: number)" {
		t.Errorf("label = %q", sh.Signatures[0].Label)
	}
}

// TestSignatureHelpUnknownToolMethodStaysNil proves a typo'd/nonexistent
// method on a real tool still yields no signature help, rather than a
// stale or wrong one.
func TestSignatureHelpUnknownToolMethodStaysNil(t *testing.T) {
	src, pos := posAtMarker(t, `
tool Project {
    read(path: string): string -> fs.read(path)
}

pipeline P {
    step S {
        var out = Project.nope(§)
    }
}
`)
	if sh := signatureHelpAt("main.mh", src, pos); sh != nil {
		t.Errorf("expected nil signature help for an undeclared method, got %+v", sh.Signatures)
	}
}
