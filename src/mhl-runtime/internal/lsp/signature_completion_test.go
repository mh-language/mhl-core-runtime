package lsp

import "testing"

const signatureCompletionTypes = `
type ReviewInput = { diff: string, base?: string }
type ReviewOutput = { approved: bool }
`

func TestPipelineParamCompletionListsParamTypeFields(t *testing.T) {
	src, pos := posAtMarker(t, signatureCompletionTypes+`
workflow Review(req: ReviewInput): ReviewOutput {
    step S {
        req.§
    }
    output: { approved: true }
}
`)
	items := completionAt("main.mh", src, pos)
	for _, field := range []string{"diff", "base"} {
		if !hasLabel(items, field) {
			t.Errorf("missing ReviewInput field %q in completion: %+v", field, items)
		}
	}
	for _, it := range items {
		if it.Label == "base" && it.Detail != "string (optional)" {
			t.Errorf("optional field detail = %q, want %q", it.Detail, "string (optional)")
		}
	}
}

func TestSignatureHeaderKeepsPipelineBlockRecognized(t *testing.T) {
	for _, header := range []string{
		"workflow Review(req: ReviewInput): ReviewOutput",
		"pipeline Review: ReviewOutput",
		"loop workflow Review(req: ReviewInput): ReviewOutput max 3",
		"partial workflow Review(req: ReviewInput)",
	} {
		t.Run(header, func(t *testing.T) {
			src, pos := posAtMarker(t, signatureCompletionTypes+header+` {
    var findings = []
    step S {
        self.§
    }
}
`)
			items := completionAt("main.mh", src, pos)
			if !hasLabel(items, "findings") {
				t.Errorf("self. completion lost the pipeline's own var under header %q: %+v", header, items)
			}
		})
	}
}

func TestSelfCompletionIncludesSignatureParam(t *testing.T) {
	src, pos := posAtMarker(t, signatureCompletionTypes+`
workflow Review(req: ReviewInput) {
    step S {
        self.§
    }
}
`)
	if items := completionAt("main.mh", src, pos); !hasLabel(items, "req") {
		t.Errorf("self. completion is missing the signature param: %+v", items)
	}
}

func TestSignatureHeaderOffersTypes(t *testing.T) {
	for _, line := range []string{
		"workflow Review(req: §",
		"workflow Review(req: ReviewInput): §",
		"pipeline Review: §",
	} {
		t.Run(line, func(t *testing.T) {
			src, pos := posAtMarker(t, signatureCompletionTypes+line+"\n")
			items := completionAt("main.mh", src, pos)
			if !hasLabel(items, "ReviewInput") || !hasLabel(items, "string") {
				t.Errorf("type completion missing in signature position %q: %+v", line, items)
			}
		})
	}
}
