package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// lintSource lints src as a fresh main.mh and returns its findings.
func lintSource(t *testing.T, src string) []lint.Finding {
	t.Helper()
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, src)
	return lint.File(main)
}

// assertFindings requires a finding containing want, or none at all when
// want is "".
func assertFindings(t *testing.T, findings []lint.Finding, want string) {
	t.Helper()
	if want == "" {
		for _, f := range findings {
			t.Errorf("unexpected finding: %s", f)
		}
		return
	}
	if !hasMessage(findings, want) {
		t.Errorf("want a finding containing %q, got %v", want, findings)
	}
}

func TestAgentInputDeclarations(t *testing.T) {
	cases := []struct {
		name, inputs, want string
	}{
		{"defaulted inputs", "input tone: string = \"x\"\n    input dirs: string[] = []", ""},
		{"run argument name", `input prompt: string = ""`, `input "prompt" clashes with run's own "prompt" argument`},
		{"declared twice", "input m: string = \"\"\n    input m: string = \"\"", `input "m" is declared twice`},
		{"unknown type", `input m: Nope = ""`, `input "m" has an unrecognized type "Nope"`},
		{"default of the wrong type", `input n: number = "x"`, `default for input "n"`},
		{"input placeholder in args", "input m: string = \"\"\n    args: [\"--tag=${m}\"]", ""},
		{"input placeholder in stdin", "input m: string = \"\"\n    stdin: \"${prompt} ${m}\"", ""},
		{"undeclared placeholder in args", "input m: string = \"\"\n    args: [\"--tag=${n}\"]", `contains ${n}, which is never substituted`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, `
agent Coder {
    `+tc.inputs+`
    command: "bash"
}

pipeline P {
    step S {
        Coder.run(prompt: "x")
    }
}
`), tc.want)
		})
	}
}

// Acceptance 6 (lint side): a call naming an undeclared input, or leaving out
// a required one, is a finding; a declared one is fine.
func TestAgentRunInputArguments(t *testing.T) {
	cases := []struct {
		name, call, want string
	}{
		{"declared inputs", `Coder.run(prompt: "x", repo: "r", model: "m")`, ""},
		{"defaulted input omitted", `Coder.run(prompt: "x", repo: "r")`, ""},
		{"undeclared name", `Coder.run(prompt: "x", repo: "r", modelo: "m")`,
			`Coder.run: unknown argument "modelo" — run accepts prompt, schema, skills, timeout and the inputs repo, model`},
		{"required input omitted", `Coder.run(prompt: "x")`, `Coder.run: missing required input "repo"`},
		{"under spawn", `spawn h = Coder.run(prompt: "x", modelo: "m")`, `unknown argument "modelo"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, `
agent Coder {
    input repo: string
    input model: string = ""
    command: "bash"
    cwd: repo
}

pipeline P {
    step S {
        `+tc.call+`
    }
}
`), tc.want)
		})
	}
}

// run forwards a call's inputs to each fallback, so a fallback must declare
// every input of its primary and may not require one a call can omit.
func TestAgentInputFallbackCompatibility(t *testing.T) {
	cases := []struct {
		name, backup, want string
	}{
		{"same inputs", "input repo: string\n    input model: string = \"x\"", ""},
		{"missing input", `input repo: string`, `fallback "Backup" does not declare input "model"`},
		{"requires an optional one", "input repo: string\n    input model: string", `fallback "Backup" requires input "model", which a call to "Coder" may omit`},
		{"requires an unknown one", "input repo: string\n    input model: string = \"\"\n    input extra: string", `fallback "Backup" requires input "extra"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, `
agent Backup {
    `+tc.backup+`
    command: "bash"
}

agent Coder {
    input repo: string
    input model: string = ""
    command: "bash"
    fallback: [Backup]
}

pipeline P {
    step S {
        Coder.run(prompt: "x", repo: "r")
    }
}
`), tc.want)
		})
	}
}

// Router.delegate may pass an input only when every agent of the router
// declares it, and must pass any agent's required input.
func TestRouterDelegateInputArguments(t *testing.T) {
	cases := []struct {
		name, call, want string
	}{
		{"common input", `Desk.delegate(prompt: "x", repo: "r")`, ""},
		{"input only one agent declares", `Desk.delegate(prompt: "x", repo: "r", tone: "warm")`,
			`Desk.delegate: unknown argument "tone" — run accepts prompt, schema, skills, timeout and the inputs repo`},
		{"required input omitted", `Desk.delegate(prompt: "x")`, `Desk.delegate: missing required input "repo"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, `
agent Billing {
    input repo: string
    input tone: string = ""
    command: "bash"
}

agent Support {
    input repo: string
    command: "bash"
}

router Desk {
    agents: [Billing, Support]
    select: (prompt) -> nameof(Support)
}

pipeline P {
    step S {
        `+tc.call+`
    }
}
`), tc.want)
		})
	}
}
