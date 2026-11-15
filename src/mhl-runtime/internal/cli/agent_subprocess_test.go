package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// These run on every platform — on Windows through cmd.exe, with no bash
// anywhere — since cwd:/log: are plain Go (exec.Cmd.Dir, a file writer), not
// a shell feature.

// runAgentSource writes src as main.mh in a fresh dir and runs it with
// `mhl run`.
func runAgentSource(t *testing.T, src string) (string, error) {
	t.Helper()
	main := filepath.Join(t.TempDir(), "main.mh")
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	return buf.String(), err
}

// mhPath renders a path for a .mh string literal (forward slashes, which
// every platform's chdir/open accept, and no backslash escapes to trip on).
func mhPath(p string) string { return filepath.ToSlash(p) }

// printCwd is a command/args pair printing the process's working directory.
func printCwd() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", `["/c", "cd"]`
	}
	return "sh", `["-c", "pwd -P"]`
}

// echoLine is a command/args pair printing line on stdout.
func echoLine(line string) (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", `["/c", "echo ` + line + `"]`
	}
	return "sh", `["-c", "echo ` + line + `"]`
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", p, err)
	}
	return filepath.Clean(r)
}

// Acceptance 1 + 2: cwd: sets the process's directory, and an expression is
// re-evaluated on every call, so two calls of one agent can run in two
// directories.
func TestAgentCwdPerCall(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	sel := filepath.Join(t.TempDir(), "selected")
	cmd, args := printCwd()
	out, err := runAgentSource(t, `
tool Repo {
    current(): string -> fs.read("`+mhPath(sel)+`").trim()
}

agent Where {
    command: "`+cmd+`"
    args: `+args+`
    cwd: Repo.current()
}

pipeline P {
    step S {
        fs.write("`+mhPath(sel)+`", "`+mhPath(a)+`")
        log.info("first=" + Where.run(prompt: "x"))
        fs.write("`+mhPath(sel)+`", "`+mhPath(b)+`")
        log.info("second=" + Where.run(prompt: "x"))
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		for _, key := range []string{"first=", "second="} {
			if i := strings.Index(line, key); i >= 0 {
				// The prompt is appended as a trailing arg; pwd/cd ignore it.
				got[key] = filepath.Clean(strings.TrimSpace(line[i+len(key):]))
			}
		}
	}
	if got["first="] != realPath(t, a) {
		t.Errorf("first call ran in %q, want %q\n%s", got["first="], realPath(t, a), out)
	}
	if got["second="] != realPath(t, b) {
		t.Errorf("second call ran in %q, want %q\n%s", got["second="], realPath(t, b), out)
	}
}

// Acceptance 1: a missing cwd fails the call before any process starts.
func TestAgentCwdMissingFailsBeforeStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	cmd, args := echoLine("started")
	out, err := runAgentSource(t, `
agent Where {
    command: "`+cmd+`"
    args: `+args+`
    cwd: "`+mhPath(missing)+`"
    retry: { max_attempts: 3, delay: 10ms }
}

pipeline P {
    step S {
        Where.run(prompt: "x")
    }
}
`)
	msg := out
	if err != nil {
		msg += err.Error()
	}
	if err == nil || !strings.Contains(msg, `agent "Where": cwd not found: `) {
		t.Fatalf("err = %v, out:\n%s", err, out)
	}
	if strings.Contains(out, "started") {
		t.Errorf("the process ran despite a missing cwd:\n%s", out)
	}
}

// Acceptance 7: log: computed per call writes that call's stdout to that
// call's file; "" writes nothing.
func TestAgentLogExpressionPerCall(t *testing.T) {
	dir := t.TempDir()
	sel := filepath.Join(dir, "selected")
	logA := filepath.Join(dir, "live", "a.log") // parent dir created on demand
	cmd, args := echoLine("hello")
	out, err := runAgentSource(t, `
tool Live {
    path(): string -> fs.read("`+mhPath(sel)+`").trim()
}

agent Talker {
    command: "`+cmd+`"
    args: `+args+`
    log: Live.path()
}

pipeline P {
    step S {
        fs.write("`+mhPath(sel)+`", "`+mhPath(logA)+`")
        Talker.run(prompt: "x")
        fs.write("`+mhPath(sel)+`", " ")
        Talker.run(prompt: "x")
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	data, err := os.ReadFile(logA)
	if err != nil {
		t.Fatalf("log not written: %v\n%s", err, out)
	}
	if n := strings.Count(string(data), "hello"); n != 1 {
		t.Errorf("log has %d lines of output, want 1 (the empty-path call must not write):\n%s", n, data)
	}
}
