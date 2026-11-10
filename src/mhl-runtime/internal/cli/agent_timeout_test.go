//go:build !windows

package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// runTimeoutSource writes src as main.mh in a fresh dir and runs it with
// `mhl run`, returning the output, the run error and how long it took.
func runTimeoutSource(t *testing.T, src string) (string, error, time.Duration) {
	t.Helper()
	main := filepath.Join(t.TempDir(), "main.mh")
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	start := time.Now()
	err := cli.Run([]string{"run", main}, &buf)
	return buf.String(), err, time.Since(start)
}

func attempts(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Count(string(data), "\n")
}

// Acceptance 1 + 6: a hung agent fails at its deadline with a message naming
// the timeout and the agent, and the surrounding catch/finally both run.
func TestAgentTimeoutFailsCallAndRunsFinally(t *testing.T) {
	out, err, elapsed := runTimeoutSource(t, `
agent Hung {
    command: "bash"
    args: ["-c", "sleep 5"]
    timeout: 300ms
}

pipeline P {
    step S {
        try {
            Hung.run(prompt: "x")
        } catch (e) {
            log.info("caught: " + e)
        } finally {
            log.info("finally ran")
        }
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, `caught: agent "Hung" timeout after 300ms`) {
		t.Errorf("missing timeout message:\n%s", out)
	}
	if !strings.Contains(out, "finally ran") {
		t.Errorf("finally did not run:\n%s", out)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s, want ~300ms", elapsed)
	}
}

// An uncaught timeout fails the run (it's an ordinary error, not a hang).
func TestAgentTimeoutUncaughtFailsRun(t *testing.T) {
	out, err, _ := runTimeoutSource(t, `
agent Hung {
    command: "bash"
    args: ["-c", "sleep 5"]
    timeout: 200ms
}

pipeline P {
    step S {
        Hung.run(prompt: "x")
    }
}
`)
	if err == nil || !strings.Contains(err.Error()+out, `agent "Hung" timeout after 200ms`) {
		t.Fatalf("err = %v, out:\n%s", err, out)
	}
}

// Acceptance 3: what the process printed while handling SIGTERM reaches the
// error, alongside the stderr tail.
func TestAgentTimeoutKeepsPartialOutput(t *testing.T) {
	out, err, elapsed := runTimeoutSource(t, `
agent Graceful {
    command: "bash"
    args: ["-c", "trap 'echo tokens=42; exit 143' TERM; echo working >&2; sleep 100 & wait"]
    timeout: 300ms
    kill_grace: 5s
}

pipeline P {
    step S {
        try {
            Graceful.run(prompt: "x")
        } catch (e) {
            log.info("caught: " + e)
        }
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, `timeout after 300ms: working`) {
		t.Errorf("missing stderr tail:\n%s", out)
	}
	if !strings.Contains(out, `partial stdout: tokens=42`) {
		t.Errorf("missing partial stdout:\n%s", out)
	}
	if elapsed > 4*time.Second {
		t.Errorf("took %s: the script exited on SIGTERM, the 5s grace should not be waited out", elapsed)
	}
}

// Acceptance 5: a timeout is retried exactly when retry_on lists "timeout" —
// and a status code in the dying process's stderr never counts as a match.
func TestAgentTimeoutRetryOn(t *testing.T) {
	cases := []struct {
		name    string
		retryOn string
		want    int
	}{
		{"timeout listed", `["timeout"]`, 2},
		{"timeout not listed", `[500, 503, "rate_limit"]`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := filepath.Join(t.TempDir(), "count")
			out, err, _ := runTimeoutSource(t, `
agent Flaky {
    command: "bash"
    args: ["-c", "echo run >> '`+count+`'; echo 'upstream 500 rate_limit' >&2; sleep 5"]
    timeout: 200ms
    retry: {
        max_attempts: 2
        delay: 10ms
        retry_on: `+tc.retryOn+`
    }
}

pipeline P {
    step S {
        try {
            Flaky.run(prompt: "x")
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
		})
	}
}

// The timeout is per attempt, not the whole call: each retry gets a fresh
// deadline, so an attempt that would finish inside it succeeds.
func TestAgentTimeoutIsPerAttempt(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "first")
	out, err, _ := runTimeoutSource(t, `
agent SlowThenFast {
    command: "bash"
    args: ["-c", "if [ -e '`+marker+`' ]; then sleep 0.3; echo done; else touch '`+marker+`'; sleep 5; fi"]
    timeout: 1s
    retry: {
        max_attempts: 2
        delay: 10ms
        retry_on: ["timeout"]
    }
}

pipeline P {
    step S {
        var r = SlowThenFast.run(prompt: "x")
        log.info("got: " + r)
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "got: done") {
		t.Errorf("second attempt should succeed inside its own deadline:\n%s", out)
	}
}

// The deadline may come from an expression (a duration string here), and a
// `.run(timeout:)` argument overrides the declared one for that call.
func TestAgentTimeoutFromExpressionAndPerCallOverride(t *testing.T) {
	out, err, elapsed := runTimeoutSource(t, `
tool AgentConfig {
    coder_timeout(): string -> "10m"
}

agent Hung {
    command: "bash"
    args: ["-c", "sleep 5"]
    timeout: AgentConfig.coder_timeout()
}

pipeline P {
    step S {
        try {
            Hung.run(prompt: "x", timeout: 250ms)
        } catch (e) {
            log.info("caught: " + e)
        }
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, `timeout after 250ms`) {
		t.Errorf("per-call timeout not applied:\n%s", out)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s", elapsed)
	}
}

// A timed-out primary falls through to the agent's fallback, like any other
// failure.
func TestAgentTimeoutTriggersFallback(t *testing.T) {
	out, err, _ := runTimeoutSource(t, `
agent Backup {
    command: "bash"
    args: ["-c", "echo from-backup"]
}

agent Hung {
    command: "bash"
    args: ["-c", "sleep 5"]
    timeout: 200ms
    fallback: [Backup]
}

pipeline P {
    step S {
        var r = Hung.run(prompt: "x")
        log.info("got: " + r)
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "got: from-backup") {
		t.Errorf("fallback not used:\n%s", out)
	}
}

func TestAgentTimeoutOllama(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "late"})
	}))
	defer srv.Close()
	defer close(release) // runs first: unblock the handler so Close doesn't wait on it
	out, err, elapsed := runTimeoutSource(t, `
agent Local {
    engine: "ollama/qwen2.5-coder"
    endpoint: "`+srv.URL+`"
    timeout: 200ms
}

pipeline P {
    step S {
        try {
            Local.run(prompt: "x")
        } catch (e) {
            log.info("caught: " + e)
        }
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, `caught: agent "Local" timeout after 200ms`) {
		t.Errorf("missing timeout message:\n%s", out)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s", elapsed)
	}
}

// A run-time timeout value that isn't a duration fails clearly.
func TestAgentTimeoutRejectsBadRuntimeValue(t *testing.T) {
	out, err, _ := runTimeoutSource(t, `
tool AgentConfig {
    coder_timeout(): string -> "soon"
}

agent Hung {
    command: "bash"
    args: ["-c", "echo hi"]
    timeout: AgentConfig.coder_timeout()
}

pipeline P {
    step S {
        Hung.run(prompt: "x")
    }
}
`)
	if err == nil || !strings.Contains(err.Error()+out, `timeout must be a duration`) {
		t.Fatalf("err = %v, out:\n%s", err, out)
	}
}
