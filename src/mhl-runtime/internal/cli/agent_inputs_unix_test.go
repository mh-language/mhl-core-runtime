//go:build !windows

package cli_test

import (
	"strings"
	"testing"
)

// runExpect runs src and requires every want in its output (or error), and
// that it fails exactly when fails is set.
func runExpect(t *testing.T, src string, fails bool, want ...string) string {
	t.Helper()
	out, err := runAgentSource(t, src)
	msg := out
	if err != nil {
		msg += err.Error()
	}
	if fails != (err != nil) {
		t.Fatalf("err = %v (fails = %v):\n%s", err, fails, msg)
	}
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("want %q in:\n%s", w, msg)
		}
	}
	return msg
}

// echoArgs is an agent body printing its argv after "argv:", one shell word
// per element, so a test can read exactly what the process received.
const echoArgs = `
    command: "sh"
    stdin: "${prompt}"
`

// Acceptance 3 + 4, in args: a spread splices a string array into the fixed
// part; [] inserts nothing; an if-expression includes or omits a pair; null
// is an error.
func TestAgentArgsSpread(t *testing.T) {
	cases := []struct {
		name, effort, want string
		fails              bool
	}{
		{"pair included", `"high"`, `argv: a --effort high b`, false},
		{"pair omitted", `""`, `argv: a b`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runExpect(t, `
tool Effort {
    flags(e: string): string[] -> if (e.is_empty()) [] else ["--effort", e]
}

agent Echo {
    input effort: string = ""`+echoArgs+`
    args: ["-c", "echo argv: $*", "sh", "a", ...Effort.flags(effort), "b"]
}

pipeline P {
    step S {
        log.info(Echo.run(prompt: "x", effort: `+tc.effort+`))
    }
}
`, tc.fails, tc.want)
		})
	}
	runExpect(t, `
tool Flags { none(): any -> null }

agent Echo {`+echoArgs+`
    args: ["-c", "echo argv: $*", "sh", ...Flags.none()]
}

pipeline P {
    step S {
        Echo.run(prompt: "x")
    }
}
`, true, `agent "Echo": args: spread (...) needs an array, got null`)
}

// Acceptance 5: an input fills args — `model` reaches the process; without
// the argument its default ("") does. A "${model}" placeholder in a literal
// args element or stdin is filled too.
func TestAgentInputsFillArgs(t *testing.T) {
	src := `
agent Echo {
    input model: string = ""
    input dirs: string[] = []
    command: "sh"
    args: ["-c", "echo argv: $* stdin: $(cat)", "sh", "--model", model, "--tag=${model}", ...dirs.flat_map((d) -> ["--add-dir", d])]
    stdin: "${prompt} for ${model}"
}

pipeline P {
    step S {
        log.info(Echo.run(prompt: "p", model: "m", dirs: ["x", "y"]))
        log.info(Echo.run(prompt: "p"))
    }
}
`
	runExpect(t, src, false,
		`argv: --model m --tag=m --add-dir x --add-dir y stdin: p for m`,
		`argv: --model --tag= stdin: p for`)
}

// Acceptance 6 (runtime side) plus the other call errors: an undeclared
// name, a missing required input, a value of the wrong type, and an input
// named after a run argument.
func TestAgentInputCallErrors(t *testing.T) {
	cases := []struct {
		name, inputs, call, want string
	}{
		{"undeclared name", `input model: string = ""`, `Echo.run(prompt: "x", modelo: "m")`,
			`Echo.run: unknown argument "modelo" — run accepts prompt, schema, skills, timeout and the inputs model`},
		{"missing required", `input repo: string`, `Echo.run(prompt: "x")`, `Echo.run: missing required input "repo"`},
		{"wrong type", `input dirs: string[] = []`, `Echo.run(prompt: "x", dirs: "a")`, `Echo.run: input "dirs"`},
		{"reserved name", `input schema: string = ""`, `Echo.run(prompt: "x")`, `input "schema" clashes with run's own "schema" argument`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runExpect(t, `
agent Echo {
    `+tc.inputs+`
    command: "sh"
    args: ["-c", "echo hi"]
}

pipeline P {
    step S {
        `+tc.call+`
    }
}
`, true, tc.want)
		})
	}
}

// A fallback receives the inputs the call passed — not the primary's
// defaults: an input the call left out takes the fallback's own default.
func TestAgentInputsForwardedToFallback(t *testing.T) {
	runExpect(t, `
agent Backup {
    input repo: string
    input model: string = "backup-default"
    command: "sh"
    args: ["-c", "echo backup repo=${repo} model=${model}"]
}

agent Main {
    input repo: string
    input model: string = "main-default"
    command: "sh"
    args: ["-c", "exit 1"]
    fallback: [Backup]
}

pipeline P {
    step S {
        log.info(Main.run(prompt: "x", repo: "r1"))
        log.info(Main.run(prompt: "x", repo: "r2", model: "m"))
    }
}
`, false, `backup repo=r1 model=backup-default`, `backup repo=r2 model=m`)
}

// Router.delegate forwards its inputs to the chosen agent.
func TestRouterDelegateForwardsInputs(t *testing.T) {
	runExpect(t, `
agent Billing {
    input repo: string
    command: "sh"
    args: ["-c", "echo billing in ${repo}"]
}

agent Support {
    input repo: string
    command: "sh"
    args: ["-c", "echo support in ${repo}"]
}

router Desk {
    agents: [Billing, Support]
    select: (prompt) -> nameof(Support)
}

pipeline P {
    step S {
        log.info(Desk.delegate(prompt: "help", repo: "r9"))
    }
}
`, false, `support in r9`)
}

// Under spawn, every handle binds its own call's inputs.
func TestAgentInputsUnderSpawn(t *testing.T) {
	runExpect(t, `
agent Echo {
    input name: string
    command: "sh"
    args: ["-c", "echo hello ${name}"]
}

pipeline P {
    step S {
        spawn a = Echo.run(prompt: "x", name: "ana")
        spawn b = Echo.run(prompt: "x", name: "bia")
        wait a, b
        log.info(a.result + " / " + b.result)
    }
}
`, false, `hello ana / hello bia`)
}

// The before/after hooks see the inputs too; the prompt: argument — the
// caller's own expression — does not (a caller variable of the same name
// stays what it was).
func TestAgentInputsInHooksNotInPrompt(t *testing.T) {
	runExpect(t, `
agent Echo {
    input who: string
    command: "sh"
    stdin: "${prompt}"
    args: ["-c", "cat"]
    before: () -> { return { greeting: "hi " + who } }
    after: () -> result + " (for " + who + ")"
}

pipeline P {
    step S {
        var who = "caller"
        log.info(Echo.run(prompt: "${greeting}; caller is ${who}", who: "callee"))
    }
}
`, false, `hi callee; caller is caller (for callee)`)
}
