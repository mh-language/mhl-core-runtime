package interpreter

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandForCwd(t *testing.T) {
	abs, _ := filepath.Abs(filepath.Join("bin", "coder"))
	cases := []struct{ command, cwd, want string }{
		{"codex", "/repo", "codex"},                   // PATH lookup: untouched
		{"./bin/coder", "", "./bin/coder"},            // no cwd: untouched
		{"./bin/coder", "/repo", abs},                 // relative to mhl, not to cwd
		{"/usr/bin/codex", "/repo", "/usr/bin/codex"}, // already absolute
	}
	for _, tc := range cases {
		if got := commandForCwd(tc.command, tc.cwd); got != tc.want {
			t.Errorf("commandForCwd(%q, %q) = %q, want %q", tc.command, tc.cwd, got, tc.want)
		}
	}
}

// The stdout tail is in the message but never in the text retry_on matches.
func TestAgentExitErrorRetryText(t *testing.T) {
	cause := errors.New("exit status 1")
	err := newAgentExitError("Coder", "boom\n", `{"error":"rate_limit"}`, cause)
	if got, want := err.Error(), "agent \"Coder\" failed: exit status 1: boom\npartial stdout: {\"error\":\"rate_limit\"}"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	rt := err.(interface{ RetryText() string }).RetryText()
	if strings.Contains(rt, "rate_limit") || rt != `agent "Coder" failed: exit status 1: boom` {
		t.Errorf("RetryText() = %q", rt)
	}
	if !errors.Is(err, cause) {
		t.Error("does not unwrap to the exec error")
	}
	if got := newAgentExitError("Coder", "", "", cause).Error(); got != `agent "Coder" failed: exit status 1` {
		t.Errorf("no output: Error() = %q", got)
	}
}
