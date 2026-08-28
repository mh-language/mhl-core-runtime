package interpreter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/adapters"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
	"github.com/mh-language/mhl-core-runtime/internal/features/tools"
	"github.com/mh-language/mhl-core-runtime/internal/features/traffic"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func findAgent(prog *ast.Program, name string) (*ast.Agent, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Agent != nil && decl.Agent.Name == name {
			return decl.Agent, true
		}
	}
	return nil, false
}

// runAgent executes agent's `.run(...)` call and returns its response. It is
// the single place this happens — reached both for a bare top-level
// `Agent.run(...)` statement and for `Agent.run(...)` used as a
// sub-expression (e.g. `var x = Agent.run(...)`) — so tracing
// ("agent %s response:\n%s\n" on ctx.out) and error behavior are identical
// either way. Tracing itself is opt-in per agent (see agentTrace): each leg
// that actually ran — the primary agent, or whichever fallback ended up
// succeeding — gates its own "response:" line on its own `trace` property,
// the same way it already has its own independent cache/retry/rate_limit.
//
// A declared `before` hook runs first (runAgentBeforeHook, agent_hooks.go):
// its returned object's fields become extra bindings visible to the
// `prompt:`/`schema:` arguments' own "${...}" interpolation (promptCtx,
// below) — this is what lets `prompt: "Review: ${mcp_result}"` resolve
// against data before/after actually fetched, not a template variable that
// happens to already exist in the calling step. A declared `after` hook
// runs once, against whichever leg (primary or fallback) produced the
// final response, immediately before this returns — not once per attempt.
func runAgent(ctx *evalCtx, agentName string, agent *ast.Agent, call *ast.Call, depth int) (string, error) {
	beforeResult, err := runAgentBeforeHook(ctx, agentName, agent, depth)
	if err != nil {
		return "", err
	}
	promptCtx := ctx
	if len(beforeResult) > 0 {
		childEnv := make(Env, len(ctx.env)+len(beforeResult))
		for k, v := range ctx.env {
			childEnv[k] = v
		}
		for k, v := range beforeResult {
			childEnv[k] = v
		}
		child := *ctx
		child.env = childEnv
		promptCtx = &child
	}

	promptText, ok, err := resolvePromptArgument(promptCtx, call, depth)
	if err != nil {
		return "", fmt.Errorf("%s.run: %w", agentName, err)
	}
	if !ok || promptText == "" {
		return "", fmt.Errorf("%s.run requires a non-empty prompt", agentName)
	}
	schemaText, _, err := resolveAgentStringArg(promptCtx, call, "schema", depth)
	if err != nil {
		return "", fmt.Errorf("%s.run: %w", agentName, err)
	}
	promptText, err = applyAgentDeclaredToolScope(ctx, agent, promptText)
	if err != nil {
		return "", fmt.Errorf("%s.run: %w", agentName, err)
	}

	response, err := runAgentAttempt(ctx, agentName, agent, promptText, schemaText)
	if err == nil {
		if agentTrace(agent) {
			fmt.Fprintf(ctx.out, "agent %s response:\n%s\n", agentName, auth.Redact(response))
		}
		return runAgentAfterHook(ctx, agentName, agent, response, depth)
	}

	fallbacks, fbErr := agentFallback(ctx.prog, agent)
	if fbErr != nil {
		return "", fbErr
	}
	for i, fb := range fallbacks {
		fbName := fb.Name
		if fbName == "" {
			fbName = fmt.Sprintf("%s.fallback[%d]", agentName, i)
		}
		response, fbAttemptErr := runAgentAttempt(ctx, fbName, fb, promptText, schemaText)
		if fbAttemptErr == nil {
			if agentTrace(fb) {
				fmt.Fprintf(ctx.out, "agent %s response (via %s):\n%s\n", agentName, fbName, auth.Redact(response))
			}
			return runAgentAfterHook(ctx, agentName, agent, response, depth)
		}
		err = fbAttemptErr // report the last fallback's error if every leg fails
	}
	return "", err
}

// resolveAgentStringArg reads call's `name:` argument as an ordinary
// expression — a variable, a json.stringify(...) call, string
// concatenation, or a plain literal (still interpolated for "${...}" spans,
// since that happens for any string literal through the normal evaluator) —
// and requires the result to be a string. Unlike resolvePromptArgument's
// plain-string branch, this doesn't restrict the argument to a literal
// string token: `prompt:` has that restriction because a bare identifier
// there would be ambiguous with the prompt-template-reference shape
// (`prompt: SecurityAuditPrompt(...)`), but `schema:` has no competing
// shape to disambiguate against, and a schema built at runtime (e.g.
// `schema: json.stringify({...})`) is the common case, not the exception.
// ok is false (with a nil error) when the named argument is absent, so a
// call that doesn't supply it behaves exactly as before this argument
// existed.
func resolveAgentStringArg(ctx *evalCtx, call *ast.Call, name string, depth int) (string, bool, error) {
	for _, arg := range call.Args {
		if arg.Name != name {
			continue
		}
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return "", false, err
		}
		s, ok := v.(string)
		if !ok {
			return "", false, fmt.Errorf("%s must be a string, got %s", name, typeName(v))
		}
		return s, true, nil
	}
	return "", false, nil
}

// runAgentAttempt executes a single agent (primary or one fallback leg)
// against its own cache and retry policy, without considering fallback —
// that's runAgent's job, since a fallback agent has its own independent
// retry/cache config and must not recurse into fallbacks of its own.
func runAgentAttempt(ctx *evalCtx, agentName string, agent *ast.Agent, promptText, schemaText string) (string, error) {
	engine, _ := agentEngine(agent)

	var command string
	var cmdArgs []string
	var endpoint, model string
	var temperature *float64

	switch {
	case engine == "" || strings.HasPrefix(engine, "cli/"):
		var cfgErr error
		command, cmdArgs, cfgErr = agentCommand(agent)
		if cfgErr != nil {
			return "", cfgErr
		}
	case strings.HasPrefix(engine, "ollama/"):
		var cfgErr error
		endpoint, model, temperature, cfgErr = agentOllamaConfig(agent, engine)
		if cfgErr != nil {
			return "", cfgErr
		}
	default:
		return "", fmt.Errorf("agent %q: engine %q is not supported yet", agentName, engine)
	}

	cache, ttl, hasCache, err := agentCache(agent)
	if err != nil {
		return "", err
	}
	var cacheKey string
	if hasCache {
		var params any
		if schemaText != "" {
			// A cache key must vary with schema — the same prompt against
			// two different schemas is two different requests, not a hit
			// on whichever one ran first (traffic.RequestKey folds this
			// straight into the SHA-256 alongside engine+prompt).
			params = schemaText
		}
		cacheKey = traffic.RequestKey(engine, promptText, params)
		if cached, hit := cache.Get(cacheKey); hit {
			if response, ok := cached.(string); ok {
				return response, nil
			}
		}
	}

	retrier, err := agentRetry(agent)
	if err != nil {
		return "", err
	}

	limiter, err := agentLimiter(agent)
	if err != nil {
		return "", err
	}

	call := func() (traffic.Result, error) {
		if err := limiter.Acquire(goctxOf(ctx)); err != nil {
			return traffic.Result{}, fmt.Errorf("agent %q: rate limit: %w", agentName, err)
		}
		defer limiter.Release()

		// A subprocess/HTTP call can easily take longer than a human's
		// patience for silent output — without this, `mhl run` gives no
		// sign anything is happening between "step: X" and the eventual
		// "agent %s response:" line, which prints only once the call has
		// already returned. This fires right before the blocking call
		// starts (never on a cache hit, which returns above before `call`
		// is ever invoked, and once per retry attempt if the agent's
		// `retry:` config causes more than one). ctx is nil only in
		// agent_test.go's direct runAgentAttempt calls, which don't care
		// about trace output. Gated on agentTrace like every other trace
		// line — off unless this agent declares `trace: true`.
		if ctx != nil && agentTrace(agent) {
			fmt.Fprintf(ctx.out, "agent %s: calling...\n", agentName)
		}

		var result tools.Result
		var runErr error
		if engine == "" || strings.HasPrefix(engine, "cli/") {
			finalArgs, argErr := injectSchemaArg(injectPromptArg(cmdArgs, promptText), schemaText)
			if argErr != nil {
				return traffic.Result{}, fmt.Errorf("agent %q: %w", agentName, argErr)
			}
			cmd := tools.Cmd{}
			// Under `spawn`, an agent's `log:` file is deliberately ignored:
			// several goroutines can be running the same agent at once, and
			// AppendWriter streams many small O_APPEND writes that would
			// interleave into one unreadable file. The subprocess output
			// still reaches the step log through ctx.out (the per-handle
			// buffer, flushed in spawn order by wait/drainAtStepEnd).
			logPath, hasLog, logErr := agentLogPath(ctx, agent)
			if logErr != nil {
				return traffic.Result{}, fmt.Errorf("agent %q: %w", agentName, logErr)
			}
			if hasLog && (ctx == nil || !ctx.inSpawn) {
				logWriter := nativeops.AppendWriter(logPath)
				defer logWriter.Close()
				cmd.Stdout = logWriter
			}
			result, runErr = (adapters.CLI{Command: cmd}).Run(goctxOf(ctx), command, finalArgs...)
		} else {
			result, runErr = (adapters.Ollama{}).Run(goctxOf(ctx), endpoint, model, promptText, temperature, schemaText)
		}
		if runErr != nil {
			detail := strings.TrimSpace(result.Stderr)
			if detail != "" {
				return traffic.Result{}, fmt.Errorf("agent %q failed: %w: %s", agentName, runErr, detail)
			}
			return traffic.Result{}, fmt.Errorf("agent %q failed: %w", agentName, runErr)
		}
		response := strings.TrimSpace(result.Stdout)
		if response == "" {
			return traffic.Result{}, fmt.Errorf("agent %q returned an empty response", agentName)
		}
		return traffic.Result{Value: response}, nil
	}

	result, err := retrier.Execute(call)
	if err != nil {
		return "", err
	}
	response := result.Value.(string)
	if hasCache {
		_ = cache.Set(cacheKey, response, ttl)
	}
	return response, nil
}

func agentCommand(agent *ast.Agent) (string, []string, error) {
	return ast.AgentCommand(agent)
}

// injectPromptArg places promptText into args at the position marked by a
// literal "${prompt}" placeholder, e.g.:
//
//	args: ["-p", "${prompt}", "--agent", "flow", "--output-format", "stream-json"]
//
// This lets a CLI agent that requires its prompt in a specific position
// (not necessarily last, e.g. immediately after a flag like `-p`) declare
// that explicitly. When no placeholder is present, promptText is appended
// at the end, preserving the previous (and still most common) behavior.
func injectPromptArg(args []string, promptText string) []string {
	for i, a := range args {
		if a == "${prompt}" {
			out := append([]string{}, args...)
			out[i] = promptText
			return out
		}
	}
	return append(args, promptText)
}

// injectSchemaArg places schemaText into args at the position marked by a
// literal "${schema}" placeholder, e.g.:
//
//	args: ["-p", "${prompt}", "--json-schema", "${schema}"]
//
// so `.run(prompt: ..., schema: ...)` can reuse one agent declaration for
// several different response shapes. Unlike injectPromptArg, there's no
// append-at-end fallback: an agent that never declared "${schema}" leaves
// args untouched even if the call supplied `schema:` (it has nowhere to go).
// The reverse — args declares "${schema}" but the call left schemaText
// empty — is an error rather than leaking the literal placeholder text
// through to the real command line, since that's the one shape a caller
// could get wrong silently (unlike `prompt`, which every call already must
// supply or runAgent rejects it before getting here).
func injectSchemaArg(args []string, schemaText string) ([]string, error) {
	for i, a := range args {
		if a == "${schema}" {
			if schemaText == "" {
				return nil, fmt.Errorf(`args declares "${schema}" but no schema: argument was supplied`)
			}
			out := append([]string{}, args...)
			out[i] = schemaText
			return out, nil
		}
	}
	return args, nil
}

// agentEngine reads the agent's engine property. ok is false when the
// property is absent or not a string.
func agentEngine(agent *ast.Agent) (string, bool) {
	return ast.AgentEngine(agent)
}

// agentLogPath reads an agent's `log` property — a file path that every
// cli/* engine call streams its subprocess's raw stdout into as it arrives
// (via tools.Cmd.Stdout / nativeops.AppendWriter), creating the file (and
// any missing parent directories) on first write rather than waiting for the
// subprocess to exit. ok is false when the agent declares no `log` property,
// letting callers skip the write entirely rather than treating "no log" and
// "empty path" the same way. Not consulted for the ollama/* engine, since
// that path calls an HTTP endpoint rather than a subprocess.
//
// The path is interpolated for "${...}" spans against ctx exactly like a
// `prompt:` string, so `log: "logs/codex.${context.session_id}.log"` gives
// each run (or each --session) its own file and two concurrent runs of the
// same pipeline never share one log. ctx is nil only in agent_test.go's
// direct calls, which pass plain paths; interpolation is skipped there.
func agentLogPath(ctx *evalCtx, agent *ast.Agent) (string, bool, error) {
	for _, prop := range agent.Props {
		if prop.Name != "log" {
			continue
		}
		raw, ok := ast.StringValue(prop.Value)
		if !ok || raw == "" {
			return "", false, nil
		}
		if ctx == nil {
			return raw, true, nil
		}
		path, err := interpolate(ctx, raw)
		if err != nil {
			return "", false, fmt.Errorf("agent log path %q: %w", raw, err)
		}
		return path, path != "", nil
	}
	return "", false, nil
}

// agentTrace reads an agent's `trace` property — when true, runAgent and
// runAgentAttempt narrate that agent's "calling..." / "response:" lines to
// ctx.out for every `.run()` against it (language-design.md §5's other
// agent properties — cache/retry/rate_limit/fallback — are all opt-in the
// same declarative way). Off by default: an agent that never declares
// `trace` runs silently on stdout, same as before this property existed —
// only its return value reaches the calling .mh code either way. ok is
// deliberately not returned (unlike agentLogPath): "absent" and "false" mean
// exactly the same thing here, so callers don't need to tell them apart.
func agentTrace(agent *ast.Agent) bool {
	for _, prop := range agent.Props {
		if prop.Name == "trace" {
			v, _ := ast.BoolValue(prop.Value)
			return v
		}
	}
	return false
}

// agentOllamaConfig reads the endpoint/temperature configuration for an
// ollama/* engine agent. model is derived from engine (the part after
// "ollama/"). temperature is nil when the agent declares no temperature
// property.
func agentOllamaConfig(agent *ast.Agent, engine string) (endpoint, model string, temperature *float64, err error) {
	return ast.AgentOllamaConfig(agent, engine)
}

// agentRetry reads an agent's `retry: { max_attempts, delay, retry_on }`
// property into a traffic.Retrier. Absent `retry` yields a Retrier that
// runs the call exactly once, so callers can use it unconditionally.
func agentRetry(agent *ast.Agent) (traffic.Retrier, error) {
	maxAttempts, delay, retryOn, err := ast.AgentRetryConfig(agent)
	if err != nil {
		return traffic.Retrier{}, err
	}
	return traffic.Retrier{MaxAttempts: maxAttempts, Delay: delay, RetryOn: retryOn}, nil
}

// agentCaches holds one long-lived *traffic.Cache per declared agent (keyed
// by the *ast.Agent node's identity, which is stable for the process's
// whole parse tree), so a `cache: {...}` agent actually accumulates hits
// across every `.run()` call for the lifetime of one `mhl run`/`mhl test`
// process — not just within a single describe block or pipeline step, both
// of which get their own fresh evalCtx (see eval.go's evalCtx doc comment).
// traffic.Cache already serializes its own reads/writes internally, so this
// map only needs to protect first-touch creation for a given agent.
var (
	agentCachesMu sync.Mutex
	agentCaches   = map[*ast.Agent]*traffic.Cache{}
)

// agentCache reads an agent's `cache: { ttl, storage }` property. hasCache
// is false when the agent declares no `cache` property at all, letting the
// caller skip the cache lookup entirely rather than treating "no cache" and
// "cache with zero TTL" the same way.
//
// Without `storage: "disk"`, entries live only in agentCaches for this
// process — real, but gone once `mhl` exits. With `storage: "disk"`, they
// also persist as JSON under .mhl-cache/, surviving across separate `mhl`
// invocations too.
//
// `strategy` other than exact-match (SHA-256 of engine+prompt+parameters)
// is rejected by ast.AgentCacheConfig before this ever runs.
func agentCache(agent *ast.Agent) (cache *traffic.Cache, ttl time.Duration, hasCache bool, err error) {
	ttl, diskStorage, hasCache, err := ast.AgentCacheConfig(agent)
	if err != nil || !hasCache {
		return nil, 0, false, err
	}
	dir := ""
	if diskStorage {
		dir = ".mhl-cache"
	}
	agentCachesMu.Lock()
	cache, ok := agentCaches[agent]
	if !ok {
		cache = traffic.NewCache(dir)
		agentCaches[agent] = cache
	}
	agentCachesMu.Unlock()
	return cache, ttl, true, nil
}

// agentLimiters holds one long-lived *traffic.Limiter per declared agent —
// same rationale and mechanics as agentCaches: a Limiter's state
// (RPM window, concurrency semaphore) only means anything if the same
// instance persists across every `.run()` call for that agent.
var (
	agentLimitersMu sync.Mutex
	agentLimiters   = map[*ast.Agent]*traffic.Limiter{}
)

// agentLimiter reads an agent's `rate_limit: { requests_per_minute,
// concurrency, on_exceeded }` property. Unlike agentCache, it always
// returns a real, non-nil *traffic.Limiter — a zero-value Limiter{} when
// the agent declares no `rate_limit` at all, or when a declared field is
// left unset. That's a genuine no-op by construction: Limiter.Acquire only
// enters the concurrency semaphore `if l.Concurrency > 0` and only enters
// the requests-per-minute wait loop `if l.RequestsPerMinute > 0`
// (rate_limit.go), so callers can call Acquire/Release unconditionally for
// every agent without a hasLimit guard.
//
// `on_exceeded` other than "queue" (Limiter.Acquire only implements block-
// and-wait) is rejected by ast.AgentLimiterConfig before this ever runs.
func agentLimiter(agent *ast.Agent) (*traffic.Limiter, error) {
	requestsPerMinute, concurrency, onExceeded, hasLimit, err := ast.AgentLimiterConfig(agent)
	if err != nil {
		return nil, err
	}
	if !hasLimit {
		return &traffic.Limiter{}, nil
	}
	limiter := &traffic.Limiter{RequestsPerMinute: requestsPerMinute, Concurrency: concurrency, OnExceeded: onExceeded}
	agentLimitersMu.Lock()
	shared, ok := agentLimiters[agent]
	if !ok {
		shared = limiter
		agentLimiters[agent] = shared
	}
	agentLimitersMu.Unlock()
	return shared, nil
}

// agentFallback reads an agent's `fallback: [...]` property. Each item is
// either an inline `agent { ... }` literal (Primary.Agent — the same node
// ast.Agent already reuses for a top-level `export agent Name {}`
// declaration, just with Name left blank), or a bare identifier naming
// another agent already declared in the program (e.g. `fallback:
// [ClaudeCLI]`), resolved the same way a top-level `ClaudeCLI.run(...)`
// statement resolves its receiver.
func agentFallback(prog *ast.Program, agent *ast.Agent) ([]*ast.Agent, error) {
	refs, err := ast.AgentFallbackRefs(agent)
	if err != nil || refs == nil {
		return nil, err
	}
	fallbacks := make([]*ast.Agent, 0, len(refs))
	for _, ref := range refs {
		if ref.Inline != nil {
			fallbacks = append(fallbacks, ref.Inline)
			continue
		}
		fb, found := findAgent(prog, ref.Name)
		if !found {
			return nil, fmt.Errorf("agent %q fallback: agent %q is not declared", agent.Name, ref.Name)
		}
		fallbacks = append(fallbacks, fb)
	}
	return fallbacks, nil
}
