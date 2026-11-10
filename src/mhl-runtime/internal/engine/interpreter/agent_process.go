package interpreter

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkAgentProps refuses to run an agent whose body carries a property the
// runtime doesn't read (ast.IsAgentProperty). mhl lint already reports it,
// but a run used to ignore it silently — an agent declaring `cwd:` before
// that property existed ran in mhl's own directory without a word — so a
// typo now fails the call instead of running the agent somewhere else, or
// with some other configuration, than the one written.
func checkAgentProps(agentName string, agent *ast.Agent) error {
	for _, p := range agent.Props {
		if !ast.IsAgentProperty(p.Name) {
			return fmt.Errorf("agent %q: unknown property %q — the runtime reads only %s", agentName, p.Name, ast.AgentPropertyList())
		}
	}
	return nil
}

// checkRunArgs rejects an `Agent.run(...)` argument the runtime doesn't read
// (ast.IsAgentRunArg) — positional, or a name like `model:` that used to be
// accepted and ignored, so the call ran with the declared model instead of
// the one asked for. Mirrored statically by lint's unknownRunArg.
func checkRunArgs(agentName string, call *ast.Call) error {
	for _, arg := range call.Args {
		if arg.Name == "" {
			return fmt.Errorf("%s.run takes named arguments only (%s)", agentName, ast.AgentRunArgList())
		}
		if !ast.IsAgentRunArg(arg.Name) {
			return fmt.Errorf("%s.run: unknown argument %q — run accepts %s", agentName, arg.Name, ast.AgentRunArgList())
		}
	}
	return nil
}

// resolveAgentCwd reads a cli/* agent's `cwd:` — the directory its process
// starts in. Like `args:` it may be any expression, evaluated on every call
// (`cwd: Repo.root()`), and a string literal interpolates "${...}" spans. A
// relative path is resolved against mhl's own working directory, and the
// directory must exist: a missing one fails the call before any process is
// started (and is never retried — the attempt never ran). Absent or "" means
// mhl's own working directory, exactly as before the property existed.
func resolveAgentCwd(ctx *evalCtx, agentName string, agent *ast.Agent, depth int) (string, error) {
	for _, p := range agent.Props {
		if p.Name != "cwd" {
			continue
		}
		dir, err := resolveAgentPathProp(ctx, agentName, "cwd", p.Value, depth)
		if err != nil || dir == "" {
			return "", err
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("agent %q: cwd %q: %w", agentName, dir, err)
		}
		info, err := os.Stat(abs)
		switch {
		case os.IsNotExist(err):
			return "", fmt.Errorf("agent %q: cwd not found: %s", agentName, abs)
		case err != nil:
			return "", fmt.Errorf("agent %q: cwd %s: %w", agentName, abs, err)
		case !info.IsDir():
			return "", fmt.Errorf("agent %q: cwd is not a directory: %s", agentName, abs)
		}
		return abs, nil
	}
	return "", nil
}

// commandForCwd keeps a relative command path (`./bin/coder`) meaning what it
// meant without `cwd:` — relative to mhl's working directory, not to the
// agent's — by making it absolute. A bare name (`codex`) is a PATH lookup and
// is left alone.
func commandForCwd(command, cwd string) string {
	if cwd == "" || filepath.IsAbs(command) || !strings.ContainsAny(command, `/\`) {
		return command
	}
	if abs, err := filepath.Abs(command); err == nil {
		return abs
	}
	return command
}

// agentLog is a cli/* agent's resolved `log:` for one call: the file its
// subprocess's stdout streams into, line by line, as it arrives.
type agentLog struct {
	path string
	// dynamic is true when the path came from an expression rather than a
	// string literal — a per-call path, which is honored even under `spawn`
	// (see runAgentAttemptWith).
	dynamic bool
	// truncate is `log_mode: "truncate"`: every attempt starts the file
	// empty, instead of appending to what earlier calls/attempts wrote.
	truncate bool
}

// resolveAgentLog reads an agent's `log:` and `log_mode:`. A string literal
// is interpolated for "${...}" spans against ctx exactly like a `prompt:`
// string (`log: "logs/codex.${context.session_id}.log"`); any other
// expression is evaluated on every call like `args:` (`log: Live.path()`) and
// must yield a string. Either way "" turns the log off for that call. ok is
// false when there is no log. ctx is nil only in agent_test.go's direct
// calls, which pass plain literal paths.
func resolveAgentLog(ctx *evalCtx, agentName string, agent *ast.Agent, depth int) (agentLog, bool, error) {
	truncate, err := ast.AgentLogMode(agent)
	if err != nil {
		return agentLog{}, false, err
	}
	for _, prop := range agent.Props {
		if prop.Name != "log" {
			continue
		}
		raw, literal := ast.StringValue(prop.Value)
		if literal {
			if raw == "" {
				return agentLog{}, false, nil
			}
			if ctx == nil {
				return agentLog{path: raw, truncate: truncate}, true, nil
			}
			path, err := interpolate(ctx, raw)
			if err != nil {
				return agentLog{}, false, fmt.Errorf("agent log path %q: %w", raw, err)
			}
			return agentLog{path: path, truncate: truncate}, path != "", nil
		}
		path, err := resolveAgentPathProp(ctx, agentName, "log", prop.Value, depth)
		if err != nil {
			return agentLog{}, false, err
		}
		return agentLog{path: path, dynamic: true, truncate: truncate}, path != "", nil
	}
	return agentLog{}, false, nil
}

// writer opens the log for one attempt. Appending is lazy (the file and its
// parent directories are created on the first line, as `log:` always did);
// truncating empties the file up front, so an attempt that prints nothing
// still doesn't leave the previous attempt's lines behind.
func (l agentLog) writer() (io.WriteCloser, error) {
	if l.truncate {
		return nativeops.TruncateWriter(l.path)
	}
	return nativeops.AppendWriter(l.path), nil
}

// resolveAgentPathProp evaluates a `cwd:`/`log:`-shaped property to a
// string: a credential reference fails closed first (and its value is
// registered for redaction), as for `args:`; the value must be a string.
func resolveAgentPathProp(ctx *evalCtx, agentName, propName string, expr *ast.Expr, depth int) (string, error) {
	if ctx == nil {
		if s, ok := ast.StringValue(expr); ok {
			return s, nil
		}
		return "", fmt.Errorf("agent %q: %s must be a string literal here", agentName, propName)
	}
	for _, ref := range ast.CredentialRefs(expr) {
		if _, err := auth.Resolve(ref); err != nil {
			return "", fmt.Errorf("agent %q: %s: %w", agentName, propName, err)
		}
	}
	v, err := evalExprAt(ctx, expr, depth)
	if err != nil {
		return "", fmt.Errorf("agent %q: %s: %w", agentName, propName, err)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("agent %q: %s must be a string, got %s", agentName, propName, typeName(v))
	}
	return s, nil
}

// agentExitError is what an attempt fails with when the process exits with a
// non-zero status (or fails to start). Error() is the message such a failure
// always had — `agent "X" failed: exit status 1: <stderr>` — followed by the
// tail of whatever the process printed on stdout, where many CLIs report the
// actual reason (a JSONL error event). RetryText() is that original message
// without the stdout, so `retry_on` matches exactly what it matched before
// and a word in the agent's own output can never trigger a retry.
type agentExitError struct {
	msg    string
	stdout string
	err    error
}

func (e *agentExitError) Error() string {
	if e.stdout == "" {
		return e.msg
	}
	return e.msg + "\npartial stdout: " + e.stdout
}

func (e *agentExitError) RetryText() string { return e.msg }

func (e *agentExitError) Unwrap() error { return e.err }

func newAgentExitError(agentName string, stderr, stdout string, cause error) error {
	msg := fmt.Sprintf("agent %q failed: %v", agentName, cause)
	if detail := strings.TrimSpace(stderr); detail != "" {
		msg += ": " + detail
	}
	return &agentExitError{msg: msg, stdout: outputTail(stdout), err: cause}
}

// resolveArgsExpr evaluates an `args:` written as a whole expression rather
// than an array literal — `args: Flags.for(model)` — which must yield an
// array of strings. Unlike an array literal's string elements, the yielded
// strings went through the ordinary evaluator, so a "${prompt}" placeholder
// can't be expressed this way: the prompt is appended last (or sent on stdin
// when `stdin:` claims it).
func resolveArgsExpr(ctx *evalCtx, agentName string, expr *ast.Expr, depth int) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("agent %q args must be an array of strings", agentName)
	}
	for _, ref := range ast.CredentialRefs(expr) {
		if _, err := auth.Resolve(ref); err != nil {
			return nil, fmt.Errorf("agent %q: args: %w", agentName, err)
		}
	}
	v, err := evalExprAt(ctx, expr, depth)
	if err != nil {
		return nil, fmt.Errorf("agent %q: args: %w", agentName, err)
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("agent %q args must be an array of strings, got %s", agentName, typeName(v))
	}
	args := make([]string, 0, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("agent %q: args[%d] must be a string, got %s", agentName, i, typeName(item))
		}
		args = append(args, s)
	}
	return args, nil
}
