package lint_test

import "testing"

func TestPromptVariantsLint(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"valid", `prompt P(x: string) {
    default { "d ${x}" }
    for "claude/*" { "c ${x}" }
}`, ""},
		{"undeclared placeholder in an arm", `prompt P(x: string) {
    default { "d ${x}" }
    for "claude/*" { "c ${y}" }
}`, `prompt "P" (for "claude/*"): template references undeclared parameter "y"`},
		{"missing arm file", `prompt P(x: string) {
    default { "d ${x}" }
    for "claude/*" from "nope.md"
}`, `prompt "P" (for "claude/*") from "nope.md"`},
		{"no default is a parse error", `prompt P() { for "x" { "x" } }`, "no default arm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, c.src), c.want)
		})
	}
}

func TestPromptTargetPlaceholderLint(t *testing.T) {
	agent := func(stdin string) string {
		return "prompt P() { default { \"d\" } for \"claude/*\" { \"c\" } }\n" +
			"agent A {\n    input model: string = \"\"\n    command: \"cat\"\n    stdin: " + stdin + "\n}\n" +
			"pipeline Q { step S { log(A.run(prompt: P())) } }\n"
	}
	cases := []struct{ name, stdin, want string }{
		{"input", `"${prompt.target(model)}"`, ""},
		{"literal", `"${prompt.target(\"claude/x\")}"`, ""},
		{"undeclared name", `"${prompt.target(modelo)}"`, `${prompt.target(modelo)} needs a declared input or a string literal`},
		{"expression form", `prompt.target("claude/" + model)`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, agent(c.stdin)), c.want)
		})
	}
}

// An omitted prompt parameter with a default is not missing (it was
// reported as "missing value for parameter" before).
func TestPromptDefaultParameterNotReportedMissing(t *testing.T) {
	src := `prompt G(name: string, lang: string = "en") { "hello ${name} (${lang})" }
agent Echo { command: "echo" }
pipeline P { step S { log(Echo.run(prompt: G(name: "a"))) } }
`
	assertFindings(t, lintSource(t, src), "")
}
