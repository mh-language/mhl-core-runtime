package lint_test

import (
	"path/filepath"
	"strings"
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

// The call-argument check covers every place a call can be written, not only
// a bare call statement in a workflow step: a tool method's expression body
// and block, a test, a lambda, an agent hook, a pipeline hook, and a call
// nested inside another expression.
func TestAgentRunArgumentsCheckedEverywhere(t *testing.T) {
	cases := []struct {
		name, decls string
	}{
		{"tool expression body", `tool Coder {
    call(p: string): string -> CopilotCoder.run(prompt: p, repo: "r", modelo: "x")
}`},
		{"tool block body", `tool Coder {
    call(p: string): string {
        return CopilotCoder.run(prompt: p, repo: "r", modelo: "x")
    }
}`},
		{"nested in another call", `tool Coder {
    call(p: string): any -> json.parse(CopilotCoder.run(prompt: p, repo: "r", modelo: "x"))
}`},
		{"inside a lambda", `tool Coder {
    all(ps: string[]): string[] -> ps.map((p) -> CopilotCoder.run(prompt: p, repo: "r", modelo: "x"))
}`},
		{"test describe", `test t {
    describe d {
        var out = CopilotCoder.run(prompt: "p", repo: "r", modelo: "x")
    }
}`},
		{"another agent's before hook", `agent Wrapper {
    command: "bash"
    before: () -> { return { draft: CopilotCoder.run(prompt: "p", repo: "r", modelo: "x") } }
}`},
		{"pipeline hook", `workflow W {
    session_start: (s) -> { CopilotCoder.run(prompt: "p", repo: "r", modelo: "x") }
    step a { log("x") }
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, `
agent CopilotCoder {
    input repo: string
    input model: string = ""
    command: "copilot"
    cwd: repo
}

`+tc.decls+`
`), `CopilotCoder.run: unknown argument "modelo"`)
		})
	}
	// A missing required input inside a tool method too.
	assertFindings(t, lintSource(t, `
agent CopilotCoder {
    input repo: string
    command: "copilot"
}

tool Coder {
    call(p: string): string -> CopilotCoder.run(prompt: p)
}
`), `CopilotCoder.run: missing required input "repo"`)
}

// A bad call is reported once, not once per check that sees it.
func TestAgentRunArgumentReportedOnce(t *testing.T) {
	findings := lintSource(t, `
agent A {
    input repo: string
    command: "bash"
}

workflow W {
    step s {
        A.run(prompt: "p", repo: "r", modelo: "x")
    }
}
`)
	n := 0
	for _, f := range findings {
		if strings.Contains(f.Message, `unknown argument "modelo"`) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("reported %d times, want 1: %v", n, findings)
	}
}

// In an agent that declares inputs, a name that is none of them — nor a
// builtin or a declared name — is reported wherever the agent reads it.
func TestAgentExpressionNames(t *testing.T) {
	cases := []struct {
		name, props, want string
	}{
		{"cwd typo", `cwd: reepo`, `undefined name "reepo"`},
		{"args element typo", `args: ["--model", modle]`, `undefined name "modle"`},
		{"spread typo", `args: ["-c", ...dirz]`, `undefined name "dirz"`},
		{"log interpolation typo", `log: "live/${reepo}.log"`, `undefined name "reepo" in "${reepo}"`},
		{"timeout expression typo", `timeout: Config.limit(modle)`, `undefined name "modle"`},
		{"before hook typo", `before: () -> { return { r: reepo } }`, `undefined name "reepo"`},
		{"inputs, declared names and builtins", "cwd: repo\n    args: [\"--model\", model, ...Config.flags(model), env(\"X\", \"\")]\n    log: \"live/${context.session_id}-${model}.log\"\n    timeout: Config.limit(model)", ""},
		{"raw literals are not interpolated", "args: [\"-p\", \"${prompt}\", \"--x=${model}\"]\n    stdin: \"${prompt}\"", ""},
		{"hook parameters", "after: () -> result + model\n    system_prompt: (skills, prompt) -> prompt + model", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := lintSource(t, `
tool Config {
    limit(m: string): string -> "30m"
    flags(m: string): string[] -> []
}

agent Coder {
    input repo: string
    input model: string = ""
    command: "copilot"
    `+tc.props+`
}

workflow W {
    step s {
        Coder.run(prompt: "p", repo: "r")
    }
}
`)
			if tc.want == "" {
				for _, f := range findings {
					t.Errorf("unexpected finding: %s", f)
				}
				return
			}
			if !hasMessage(findings, tc.want) {
				t.Fatalf("want a finding containing %q, got %v", tc.want, findings)
			}
			for _, f := range findings {
				if f.Line <= 1 {
					t.Errorf("finding without a real position: %s", f)
				}
			}
		})
	}
}

// An agent without inputs still reads the caller's scope, as documented
// (`log: "run.${run_tag}.log"`), so its names are not checked.
func TestAgentWithoutInputsKeepsCallerScope(t *testing.T) {
	assertFindings(t, lintSource(t, `
agent Logger {
    command: "echo"
    log: "logs/run.${run_tag}.log"
    cwd: workdir
}

workflow W {
    step s {
        var run_tag = "a"
        var workdir = "."
        Logger.run(prompt: "p")
    }
}
`), "")
}

// Every argument problem of one call is reported, not just the first: a
// misspelled input is unknown *and* leaves the real one missing.
func TestAgentRunReportsEveryArgumentProblem(t *testing.T) {
	findings := lintSource(t, `
agent Coder {
    input repo: string
    input dirs: string[] = []
    command: "copilot"
}

tool T {
    call(p: string): string -> Coder.run(prompt: p, repoo: "r", modelo: "m")
}
`)
	for _, want := range []string{
		`Coder.run: unknown argument "repoo"`,
		`Coder.run: unknown argument "modelo"`,
		`Coder.run: missing required input "repo"`,
	} {
		if !hasMessage(findings, want) {
			t.Errorf("missing %q in %v", want, findings)
		}
	}
	if len(findings) != 3 {
		t.Errorf("want 3 findings, got %d: %v", len(findings), findings)
	}
}

// A literal input value is checked against the input's declared type, like
// the run does; a computed one is left to the run.
func TestAgentRunInputLiteralTypes(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"number for a string", `repo: 42`, `Coder.run: input "repo" must be string, got number`},
		{"string for an array", `repo: "r", dirs: "a"`, `Coder.run: input "dirs" must be string[]`},
		{"wrong element type", `repo: "r", dirs: ["a", 1]`, `Coder.run: input "dirs"`},
		{"right types", `repo: "r", dirs: ["a"], n: 3`, ""},
		{"computed value", `repo: Cfg.repo(), n: Cfg.n()`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, `
tool Cfg {
    repo(): any -> 42
    n(): any -> "x"
}

agent Coder {
    input repo: string
    input dirs: string[] = []
    input n: number = 1
    command: "copilot"
}

tool T {
    call(p: string): string -> Coder.run(prompt: p, `+tc.args+`)
}
`), tc.want)
		})
	}
}

// A spread of a literal that is not an array always fails at run time, in any
// array; in args, every literal element — spread or not — must be a string.
func TestSpreadAndArgsLiterals(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"spread of a number", `workflow W { step s { log(["a", ...42]) } }`, `spread (...) needs an array, got number`},
		{"spread of null", `workflow W { step s { log([...null]) } }`, `spread (...) needs an array, got null`},
		{"spread of a literal array", `workflow W { step s { log(["a", ...["b"]]) } }`, ""},
		{"args spread with a number", "agent A {\n    command: \"sh\"\n    args: [\"-c\", ...[\"x\", 1]]\n}", `agent "A": args: element 1 of a spread (...) must be a string, got number`},
		{"args number element", "agent A {\n    command: \"sh\"\n    args: [\"-n\", 3]\n}", `agent "A": args[1] must be a string, got number`},
		{"args computed elements", "agent A {\n    input n: string = \"3\"\n    command: \"sh\"\n    args: [\"-n\", n, ...[n]]\n}", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFindings(t, lintSource(t, tc.src+"\n"), tc.want)
		})
	}
}
