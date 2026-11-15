package interpreter

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mh-language/mhl-core-runtime/internal/features/tools"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// agentTimeoutConfig is one agent leg's per-attempt deadline: `timeout:`
// bounds every attempt the leg's `retry:` policy makes (worst case is
// max_attempts × timeout plus the backoff waits), and `kill_grace:` is how
// long a timed-out cli/* process tree gets between SIGTERM and SIGKILL.
// A zero timeout means the attempt has no deadline — an agent without
// `timeout:` behaves exactly as before the property existed.
type agentTimeoutConfig struct {
	timeout   time.Duration
	killGrace time.Duration
}

// agentCallOptions carries what one `.run(...)` call overrides for every
// leg it ends up running (the primary agent and any fallback).
type agentCallOptions struct {
	// timeout, when positive, replaces each leg's own `timeout:` for this
	// call only (`Agent.run(prompt: ..., timeout: 45m)`).
	timeout time.Duration
}

// agentTimeout reads agent's `timeout:` and `kill_grace:` properties. Each is
// a duration literal (`30m`), or any expression — evaluated against ctx like
// `args:` — yielding a duration, a duration string ("30m"), or a number of
// seconds, so the deadline can come from configuration
// (`timeout: AgentConfig.coder_timeout()`). timeout must be positive;
// kill_grace may be 0 (kill at once) and defaults to tools.DefaultKillGrace.
func agentTimeout(ctx *evalCtx, agentName string, agent *ast.Agent, depth int) (agentTimeoutConfig, error) {
	cfg := agentTimeoutConfig{killGrace: tools.DefaultKillGrace}
	for _, prop := range agent.Props {
		switch prop.Name {
		case "timeout":
			d, err := agentDurationProp(ctx, agentName, prop.Name, prop.Value, depth)
			if err != nil {
				return cfg, err
			}
			if d <= 0 {
				return cfg, fmt.Errorf("agent %q: timeout must be a positive duration", agentName)
			}
			cfg.timeout = d
		case "kill_grace":
			d, err := agentDurationProp(ctx, agentName, prop.Name, prop.Value, depth)
			if err != nil {
				return cfg, err
			}
			if d < 0 {
				return cfg, fmt.Errorf("agent %q: kill_grace must not be negative", agentName)
			}
			cfg.killGrace = d
		}
	}
	return cfg, nil
}

// agentDurationProp resolves one duration-valued agent property. A bare
// duration literal is read statically (so a nil ctx — direct
// runAgentAttempt calls in tests — still works); anything else is evaluated.
func agentDurationProp(ctx *evalCtx, agentName, propName string, expr *ast.Expr, depth int) (time.Duration, error) {
	if d, ok := ast.DurationValue(expr); ok {
		return d, nil
	}
	if ctx == nil {
		return 0, fmt.Errorf("agent %q: %s must be a duration literal (e.g. 30m)", agentName, propName)
	}
	v, err := evalExprAt(ctx, expr, depth)
	if err != nil {
		return 0, fmt.Errorf("agent %q: %s: %w", agentName, propName, err)
	}
	d, err := durationFromValue(v)
	if err != nil {
		return 0, fmt.Errorf("agent %q: %s %w", agentName, propName, err)
	}
	return d, nil
}

// durationFromValue converts an evaluated value to a duration: a duration
// literal's own value, a string in the same notation ("30m", "90s",
// "250ms"), or a number of seconds.
func durationFromValue(v any) (time.Duration, error) {
	switch t := v.(type) {
	case time.Duration:
		return t, nil
	case string:
		if d, ok := ast.ParseDuration(strings.TrimSpace(t)); ok {
			return d, nil
		}
		return 0, fmt.Errorf("must be a duration (e.g. \"30m\"), got %q", t)
	case float64:
		return time.Duration(t * float64(time.Second)), nil
	}
	return 0, fmt.Errorf("must be a duration, a duration string or a number of seconds, got %s", typeName(v))
}

// resolveCallTimeoutArg reads a `.run(..., timeout: <dur>)` argument — the
// same value forms agentTimeout accepts. ok is false when the call has none.
func resolveCallTimeoutArg(ctx *evalCtx, agentName string, call *ast.Call, depth int) (time.Duration, error) {
	for _, arg := range call.Args {
		if arg.Name != "timeout" {
			continue
		}
		var d time.Duration
		if lit, ok := ast.DurationValue(arg.Value); ok {
			d = lit
		} else {
			v, err := evalExprAt(ctx, arg.Value, depth)
			if err != nil {
				return 0, fmt.Errorf("%s.run: timeout: %w", agentName, err)
			}
			if d, err = durationFromValue(v); err != nil {
				return 0, fmt.Errorf("%s.run: timeout %w", agentName, err)
			}
		}
		if d <= 0 {
			return 0, fmt.Errorf("%s.run: timeout must be a positive duration", agentName)
		}
		return d, nil
	}
	return 0, nil
}

// agentOutputTailBytes bounds how much captured output a timeout error
// carries, so a chatty process can't turn one error into megabytes.
const agentOutputTailBytes = 2048

// agentTimeoutError is what one agent attempt fails with when its deadline
// elapses. Error() reads `agent "X" timeout after 30m: <stderr tail>`,
// followed by the tail of the stdout received before the process was stopped
// (a CLI's last event often carries token/cost totals worth recording even
// for an aborted call). RetryText() is just "timeout", so `retry_on` decides
// on the timeout alone, never on whatever the dying process last printed. It
// unwraps to tools.ErrTimeout.
type agentTimeoutError struct {
	agent  string
	after  time.Duration
	stderr string
	stdout string
	err    error
}

func (e *agentTimeoutError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent %q timeout after %s", e.agent, tools.FormatDuration(e.after))
	if e.stderr != "" {
		b.WriteString(": ")
		b.WriteString(e.stderr)
	}
	if e.stdout != "" {
		b.WriteString("\npartial stdout: ")
		b.WriteString(e.stdout)
	}
	return b.String()
}

func (e *agentTimeoutError) RetryText() string { return "timeout" }

func (e *agentTimeoutError) Unwrap() error { return e.err }

func newAgentTimeoutError(agentName string, after time.Duration, result tools.Result, cause error) error {
	return &agentTimeoutError{
		agent:  agentName,
		after:  after,
		stderr: outputTail(result.Stderr),
		stdout: outputTail(result.Stdout),
		err:    cause,
	}
}

// outputTail is the last agentOutputTailBytes of s, trimmed, starting on a
// rune boundary and marked with a leading "…" when cut.
func outputTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= agentOutputTailBytes {
		return s
	}
	s = s[len(s)-agentOutputTailBytes:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return "…" + s
}
