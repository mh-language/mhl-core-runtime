package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestAgentTimeoutValues(t *testing.T) {
	cases := []struct {
		name  string
		props string
		want  string // "" means no finding mentioning timeout/kill_grace
	}{
		{"duration literal", `timeout: 30m`, ""},
		{"with grace", "timeout: 30m\n    kill_grace: 5s", ""},
		{"zero grace", "timeout: 1s\n    kill_grace: 0s", ""},
		{"duration string", `timeout: "45m"`, ""},
		{"seconds number", `timeout: 90`, ""},
		{"expression", `timeout: env("CODER_TIMEOUT", "30m")`, ""},
		{"interpolated string", `timeout: "${minutes}m"`, ""},
		{"zero timeout", `timeout: 0s`, `timeout must be a positive duration`},
		{"bad string", `timeout: "soon"`, `timeout must be a duration`},
		{"bool", `timeout: true`, `timeout must be a duration`},
		{"grace without timeout", `kill_grace: 5s`, `kill_grace has no effect without timeout`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.mh")
			write(t, main, `
agent Coder {
    command: "bash"
    args: ["-c", "cat"]
    `+tc.props+`
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
