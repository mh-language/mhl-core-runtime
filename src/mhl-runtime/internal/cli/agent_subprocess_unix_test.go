//go:build !windows

package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A relative cwd is resolved against mhl's own working directory, and a
// relative command keeps meaning a path relative to it too — not to cwd.
func TestAgentCwdRelative(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "bin", "where")
	if err := os.WriteFile(script, []byte("#!/bin/sh\npwd -P\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	out, err := runAgentSource(t, `
agent Where {
    command: "./bin/where"
    cwd: "repo"
}

pipeline P {
    step S {
        log.info("in=" + Where.run(prompt: "x"))
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if want := "in=" + realPath(t, filepath.Join(root, "repo")); !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
}

// args: may be a whole expression yielding the array; anything but an array
// of strings is a clear error.
func TestAgentArgsExpression(t *testing.T) {
	cases := []struct {
		name, method, want string
		fails              bool
	}{
		{"array of strings", `flags(): string[] -> ["-c", "echo from-expr"]`, "got: from-expr", false},
		{"not an array", `flags(): string -> "-c"`, `args must be an array of strings, got string`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runAgentSource(t, `
tool Flags {
    `+tc.method+`
}

agent Echo {
    command: "sh"
    args: Flags.flags()
    stdin: "${prompt}"
}

pipeline P {
    step S {
        log.info("got: " + Echo.run(prompt: "x"))
    }
}
`)
			msg := out
			if err != nil {
				msg += err.Error()
			}
			if tc.fails != (err != nil) || !strings.Contains(msg, tc.want) {
				t.Fatalf("err = %v, want %q in:\n%s", err, tc.want, msg)
			}
		})
	}
}

// Acceptance 8: with log_mode: "truncate", the second attempt's log holds only
// its own output; the default ("append") keeps both.
func TestAgentLogModeTruncatePerAttempt(t *testing.T) {
	cases := []struct {
		mode       string
		wantFirst  bool
		wantSecond bool
	}{
		{`log_mode: "truncate"`, false, true},
		{``, true, true},
	}
	for _, tc := range cases {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "live.log")
			marker := filepath.Join(dir, "first")
			out, err := runAgentSource(t, `
agent Flaky {
    command: "sh"
    args: ["-c", "if [ -e '`+marker+`' ]; then echo second-attempt; else touch '`+marker+`'; echo first-attempt; exit 1; fi"]
    log: "`+logPath+`"
    `+tc.mode+`
    retry: { max_attempts: 2, delay: 10ms }
}

pipeline P {
    step S {
        Flaky.run(prompt: "x")
    }
}
`)
			if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			data, _ := os.ReadFile(logPath)
			if got := strings.Contains(string(data), "first-attempt"); got != tc.wantFirst {
				t.Errorf("first attempt in log = %v, want %v:\n%s", got, tc.wantFirst, data)
			}
			if got := strings.Contains(string(data), "second-attempt"); got != tc.wantSecond {
				t.Errorf("second attempt in log = %v, want %v:\n%s", got, tc.wantSecond, data)
			}
		})
	}
}

// Under spawn, a literal log: is still skipped (concurrent calls would share
// one file), but a per-call (expression) path is honored.
func TestAgentLogUnderSpawn(t *testing.T) {
	dir := t.TempDir()
	literal := filepath.Join(dir, "literal.log")
	dynamic := filepath.Join(dir, "dynamic.log")
	out, err := runAgentSource(t, `
tool Live {
    path(): string -> "`+dynamic+`"
}

agent Literal {
    command: "sh"
    args: ["-c", "echo lit"]
    log: "`+literal+`"
}

agent Dynamic {
    command: "sh"
    args: ["-c", "echo dyn"]
    log: Live.path()
}

pipeline P {
    step S {
        spawn a = Literal.run(prompt: "x")
        spawn b = Dynamic.run(prompt: "x")
        wait a, b
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if _, err := os.Stat(literal); !os.IsNotExist(err) {
		t.Errorf("literal log was written under spawn (err=%v)", err)
	}
	if data, _ := os.ReadFile(dynamic); !strings.Contains(string(data), "dyn") {
		t.Errorf("dynamic log not written under spawn: %q\n%s", data, out)
	}
}

// Acceptance 9: a non-zero exit carries the stdout tail in its message, and
// retry_on still matches only what it matched before (stderr, exit status),
// never the stdout.
func TestAgentExitErrorPartialStdout(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   int
	}{
		{"503 only on stdout", `echo '{"type":"error","status":503}'; exit 1`, 1},
		{"503 on stderr", `echo 'upstream 503' >&2; exit 1`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := filepath.Join(t.TempDir(), "count")
			out, err := runAgentSource(t, `
agent Failing {
    command: "sh"
    args: ["-c", "echo run >> '`+count+`'; `+strings.ReplaceAll(tc.script, `"`, `\"`)+`"]
    retry: { max_attempts: 2, delay: 10ms, retry_on: [503] }
}

pipeline P {
    step S {
        try {
            Failing.run(prompt: "x")
        } catch (e) {
            log.info("caught: " + e)
        }
    }
}
`)
			if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			if got := attempts(t, count); got != tc.want {
				t.Errorf("attempts = %d, want %d\n%s", got, tc.want, out)
			}
			if !strings.Contains(out, `caught: agent "Failing" failed: exit status 1`) {
				t.Errorf("missing exit message:\n%s", out)
			}
			if strings.Contains(tc.script, `"type"`) && !strings.Contains(out, `partial stdout: {"type":"error","status":503}`) {
				t.Errorf("missing partial stdout:\n%s", out)
			}
		})
	}
}

// Acceptance 10: what used to be silently ignored now fails the call — an
// unknown agent property, and an argument run doesn't read.
func TestAgentSilentFailuresAreErrors(t *testing.T) {
	cases := []struct {
		name, agent, call, want string
	}{
		{"unknown property", `cwdir: "/tmp"`, `Echo.run(prompt: "x")`, `agent "Echo": unknown property "cwdir"`},
		{"unknown run argument", ``, `Echo.run(prompt: "x", model: "m")`, `Echo.run: unknown argument "model"`},
		{"non-string log", `log: 42`, `Echo.run(prompt: "x")`, `log must be a string, got number`},
		{"bad log_mode", `log: "x.log"
    log_mode: "rotate"`, `Echo.run(prompt: "x")`, `log_mode must be "append" or "truncate"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runAgentSource(t, `
agent Echo {
    command: "sh"
    args: ["-c", "echo hi"]
    `+tc.agent+`
}

pipeline P {
    step S {
        `+tc.call+`
    }
}
`)
			msg := out
			if err != nil {
				msg += err.Error()
			}
			if err == nil || !strings.Contains(msg, tc.want) {
				t.Fatalf("err = %v, want %q in:\n%s", err, tc.want, msg)
			}
		})
	}
}
