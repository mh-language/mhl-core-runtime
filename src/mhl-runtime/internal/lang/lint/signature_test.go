package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

const signatureTypes = `
type Finding = { file: string, message: string }
type ReviewInput = { diff: string, base?: string }
type ReviewOutput = { approved: bool, findings: Finding[], summary: string }
`

func TestSignatureValid(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, signatureTypes+`
workflow Review(req: ReviewInput): ReviewOutput {
    var findings = []
    step S {
        var base = req.base ?? "main"
        log("${req.diff} ${base}")
    }
    output: { approved: true, findings: findings, summary: "ok" }
}

pipeline OnlyIn(req: ReviewInput) {
    step S { log(req.diff) }
}

pipeline OnlyOut: { ok: bool } {
    step S { log("x") }
    output: { ok: true }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for a valid typed signature, got %+v", findings)
	}
}

func TestSignatureFindings(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "result type without output",
			src:  `workflow W: ReviewOutput { step S { log("x") } }`,
			want: `workflow "W" declares result type ReviewOutput but no ` + "`output: { ... }`" + ` projection`,
		},
		{
			name: "output literal missing a required field",
			src: `workflow W: ReviewOutput {
    step S { log("x") }
    output: { approved: true, findings: [] }
}`,
			want: `workflow "W": output is missing field "summary" required by ReviewOutput`,
		},
		{
			name: "output literal with an undeclared field",
			src: `workflow W: ReviewOutput {
    step S { log("x") }
    output: { approved: true, findings: [], summary: "", extra: 1 }
}`,
			want: `workflow "W": output field "extra" is not declared in ReviewOutput`,
		},
		{
			name: "non-object result type",
			src: `workflow W: string {
    step S { log("x") }
    output: { a: 1 }
}`,
			want: `workflow "W": result type must be an object type`,
		},
		{
			name: "unknown result type",
			src: `workflow W: Nope {
    step S { log("x") }
    output: { a: 1 }
}`,
			want: `workflow "W": unrecognized result type "Nope"`,
		},
		{
			name: "non-object param type",
			src:  `workflow W(req: string) { step S { log(req) } }`,
			want: `workflow "W": parameter "req" must be an object type`,
		},
		{
			name: "unknown param type",
			src:  `workflow W(req: Nope) { step S { log("x") } }`,
			want: `workflow "W": parameter "req": unrecognized type "Nope"`,
		},
		{
			name: "param mixed with per-line inputs",
			src: `workflow W(req: ReviewInput) {
    input extra: string
    step S { log(extra) }
}`,
			want: "workflow \"W\": `input extra` can't be combined with the typed parameter \"req\"",
		},
		{
			name: "assigning the param",
			src:  "workflow W(req: ReviewInput) {\n    step S { req = {diff: \"y\"} }\n}",
			want: `cannot assign to "req": the typed parameter is read-only`,
		},
		{
			name: "assigning the param through self",
			src:  "workflow W(req: ReviewInput) {\n    step S { self.req = 1 }\n}",
			want: `cannot assign to "req": the typed parameter is read-only`,
		},
		{
			name: "assigning a field of the param by index",
			src:  "workflow W(req: ReviewInput) {\n    step S { req[\"diff\"] = \"y\" }\n}",
			want: `cannot assign to "req": the typed parameter is read-only`,
		},
		{
			name: "step-local var shadows the param",
			src:  "workflow W(req: ReviewInput) {\n    step S {\n        var req = 1\n    }\n}",
			want: `"req" is the typed parameter and can't be redeclared`,
		},
		{
			name: "pipeline var shadows the param",
			src: `workflow W(req: ReviewInput) {
    var req = 1
    step S { log("x") }
}`,
			want: `workflow "W": a pipeline-level declaration shadows the typed parameter "req"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			main := filepath.Join(t.TempDir(), "main.mh")
			write(t, main, signatureTypes+tc.src+"\n")
			findings := lint.File(main)
			if !hasMessage(findings, tc.want) {
				t.Fatalf("expected a finding containing %q, got %+v", tc.want, findings)
			}
		})
	}
}

func TestSignatureOnTwoPartialFragmentsFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, signatureTypes+`
partial workflow W(req: ReviewInput) {
    entry step A { goto B }
}
partial workflow W(req: ReviewInput) {
    step B { log("b") }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `partial workflow "W": a typed signature is declared on more than one fragment`) {
		t.Fatalf("expected a duplicate-signature finding, got %+v", findings)
	}
}
