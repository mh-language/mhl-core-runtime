package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Like agent_subprocess_test.go, these run on every platform — on Windows
// through cmd.exe, with no bash anywhere.

// Acceptance 2: cwd: reading an input moves the process between two calls of
// the same agent.
func TestAgentInputCwdPerCall(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	cmd, args := printCwd()
	out, err := runAgentSource(t, `
agent Where {
    input repo: string
    command: "`+cmd+`"
    args: `+args+`
    cwd: repo
}

pipeline P {
    step S {
        log.info("first=" + Where.run(prompt: "x", repo: "`+mhPath(a)+`"))
        log.info("second=" + Where.run(prompt: "x", repo: "`+mhPath(b)+`"))
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for key, dir := range map[string]string{"first=": a, "second=": b} {
		if want := key + realPath(t, dir); !containsLineWithPrefix(out, want) {
			t.Errorf("want a line with %q in:\n%s", want, out)
		}
	}
}

// Acceptance 7: log: reading an input writes that call's stdout to that
// call's file; "" (the input's default) writes nothing.
func TestAgentInputLogPerCall(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "live", "first.log")
	cmd, args := echoLine("hello")
	out, err := runAgentSource(t, `
agent Talker {
    input live: string = ""
    command: "`+cmd+`"
    args: `+args+`
    log: live
}

pipeline P {
    step S {
        Talker.run(prompt: "x", live: "`+mhPath(first)+`")
        Talker.run(prompt: "x")
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("log not written: %v\n%s", err, out)
	}
	if n := strings.Count(string(data), "hello"); n != 1 {
		t.Errorf("log has %d lines of output, want 1 (the call with live = \"\" must not write):\n%s", n, data)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "live"))
	if len(entries) != 1 {
		t.Errorf("live/ has %d files, want 1", len(entries))
	}
}

// Acceptance 3, for any array literal: a spread inserts the elements of an
// array — none for [] — and a spread of null is an error.
func TestArraySpread(t *testing.T) {
	cases := []struct {
		name, xs, want string
		fails          bool
	}{
		{"two elements", `["x", "y"]`, `got=["a","x","y","b"]`, false},
		{"empty", `[]`, `got=["a","b"]`, false},
		{"null", `null`, `spread (...) needs an array, got null`, true},
		{"not an array", `"xy"`, `spread (...) needs an array, got string`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runAgentSource(t, `
pipeline P {
    step S {
        var xs = `+tc.xs+`
        log.info("got=" + json.stringify(["a", ...xs, "b"]))
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

// Acceptance 4: a spread of an if-expression includes or omits a pair.
func TestArraySpreadOfIfExpression(t *testing.T) {
	out, err := runAgentSource(t, `
tool Effort {
    flags(e: string): string[] -> ["--model", "m", ...(if (e.is_empty()) [] else ["--effort", e])]
}

pipeline P {
    step S {
        log.info("with=" + Effort.flags("high").join(" "))
        log.info("without=" + Effort.flags("").join(" "))
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"with=--model m --effort high", "without=--model m\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

// flat_map concatenates the array each element's transform returns; a
// non-array result is an error.
func TestArrayFlatMap(t *testing.T) {
	out, err := runAgentSource(t, `
pipeline P {
    step S {
        log.info("got=" + json.stringify(["a", "b"].flat_map((d) -> ["--add-dir", d])))
        log.info("empty=" + json.stringify([].flat_map((d) -> [d])))
        try {
            ["a"].flat_map((d) -> d)
        } catch (e) {
            log.info("caught: " + e)
        }
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{
		`got=["--add-dir","a","--add-dir","b"]`,
		`empty=[]`,
		`caught: flat_map() transform must return an array, got string for element 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func containsLineWithPrefix(out, prefix string) bool {
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, prefix); i >= 0 {
			// The prompt is appended as a trailing arg; pwd/cd ignore it.
			rest := strings.TrimSpace(line[i:])
			if filepath.Clean(rest) == filepath.Clean(prefix) || rest == prefix {
				return true
			}
		}
	}
	return false
}
