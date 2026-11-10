package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestAgentSubprocessProperties(t *testing.T) {
	cases := []struct {
		name  string
		props string
		want  string // "" means no finding at all
	}{
		{"cwd literal", `cwd: "repo"`, ""},
		{"cwd expression", `cwd: env("REPO", ".")`, ""},
		{"cwd number", `cwd: 42`, `cwd must be a string`},
		{"log expression", `log: env("LIVE_LOG", "")`, ""},
		{"log number", `log: 42`, `log must be a string`},
		{"log_mode truncate", "log: \"a.log\"\n    log_mode: \"truncate\"", ""},
		{"log_mode append", "log: \"a.log\"\n    log_mode: \"append\"", ""},
		{"log_mode bad", "log: \"a.log\"\n    log_mode: \"rotate\"", `log_mode must be "append" or "truncate"`},
		{"log_mode without log", `log_mode: "truncate"`, `log_mode has no effect without log`},
		{"args expression", `args: env("FLAGS", "")`, ""},
		{"args string literal", `args: "-c"`, `args must be an array of strings`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			main := filepath.Join(t.TempDir(), "main.mh")
			write(t, main, `
agent Coder {
    command: "bash"
    `+tc.props+`
}

pipeline P {
    step S {
        Coder.run(prompt: "x")
    }
}
`)
			findings := lint.File(main)
			if tc.want == "" {
				for _, f := range findings {
					t.Errorf("unexpected finding: %s", f)
				}
				return
			}
			if !hasMessage(findings, tc.want) {
				t.Fatalf("expected %q, got %+v", tc.want, findings)
			}
		})
	}
}

func TestAgentCwdOnOllamaIsFlagged(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, `
agent Local {
    engine: "ollama/qwen2.5-coder"
    endpoint: "http://localhost:11434"
    cwd: "repo"
}
`)
	if findings := lint.File(main); !hasMessage(findings, `cwd applies only to a subprocess (cli/*) agent`) {
		t.Fatalf("got %+v", findings)
	}
}

// Acceptance 6: an argument run doesn't read is a lint error.
func TestAgentRunUnknownArgument(t *testing.T) {
	cases := []struct {
		call, want string
	}{
		{`Coder.run(prompt: "x", modelo: "m")`, `Coder.run: unknown argument "modelo" — run accepts prompt, schema, skills, timeout`},
		{`Coder.run("x")`, `Coder.run takes named arguments only`},
		{`Coder.run(prompt: "x", schema: "{}", timeout: 5m)`, ``},
	}
	for _, tc := range cases {
		t.Run(tc.call, func(t *testing.T) {
			main := filepath.Join(t.TempDir(), "main.mh")
			write(t, main, `
agent Coder {
    command: "bash"
}

pipeline P {
    step S {
        `+tc.call+`
    }
}
`)
			findings := lint.File(main)
			if tc.want == "" {
				for _, f := range findings {
					t.Errorf("unexpected finding: %s", f)
				}
				return
			}
			if !hasMessage(findings, tc.want) {
				t.Fatalf("expected %q, got %+v", tc.want, findings)
			}
		})
	}
}

func TestRouterDelegateUnknownArgument(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main.mh")
	write(t, main, `
agent A {
    command: "bash"
}

router R {
    agents: [A]
    select: (prompt) -> nameof(A)
}

pipeline P {
    step S {
        R.delegate(prompt: "x", model: "m")
    }
}
`)
	if findings := lint.File(main); !hasMessage(findings, `R.delegate: unknown argument "model"`) {
		t.Fatalf("got %+v", findings)
	}
}
