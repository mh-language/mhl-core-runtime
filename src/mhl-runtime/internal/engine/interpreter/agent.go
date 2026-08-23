package interpreter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/features/adapters"
	"github.com/yanjustino/mhl-runtime/internal/features/nativeops"
	"github.com/yanjustino/mhl-runtime/internal/features/tools"
	"github.com/yanjustino/mhl-runtime/internal/features/traffic"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

func findAgent(prog *ast.Program, name string) (*ast.Agent, bool) {
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
// either way.
func runAgent(ctx *evalCtx, agentName string, agent *ast.Agent, call *ast.Call, depth int) (string, error) {
	promptText, ok, err := resolvePromptArgument(ctx, call)
	if err != nil {
		return "", fmt.Errorf("%s.run: %w", agentName, err)
	}
	if !ok || promptText == "" {
		return "", fmt.Errorf("%s.run requires a non-empty prompt", agentName)
	}
	schemaText, _, err := resolveAgentStringArg(ctx, call, "schema", depth)
	if err != nil {
		return "", fmt.Errorf("%s.run: %w", agentName, err)
	}

	response, err := runAgentAttempt(ctx, agentName, agent, promptText, schemaText)
	if err == nil {
		fmt.Fprintf(ctx.out, "agent %s response:\n%s\n", agentName, response)
		return response, nil
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
			fmt.Fprintf(ctx.out, "agent %s response (via %s):\n%s\n", agentName, fbName, response)
			return response, nil
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
		if err := limiter.Acquire(context.Background()); err != nil {
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
		// about trace output.
		if ctx != nil {
			fmt.Fprintf(ctx.out, "agent %s: calling...\n", agentName)
		}

		var result tools.Result
		var runErr error
		if engine == "" || strings.HasPrefix(engine, "cli/") {
			finalArgs, argErr := injectSchemaArg(injectPromptArg(cmdArgs, promptText), schemaText)
			if argErr != nil {
				return traffic.Result{}, fmt.Errorf("agent %q: %w", agentName, argErr)
			}
			result, runErr = (adapters.CLI{Command: tools.Cmd{}}).Run(context.Background(), command, finalArgs...)
			if logPath, hasLog := agentLogPath(agent); hasLog && result.Stdout != "" {
				if _, logErr := nativeops.Append(logPath, result.Stdout); logErr != nil {
					return traffic.Result{}, fmt.Errorf("agent %q: writing log: %w", agentName, logErr)
				}
			}
		} else {
			result, runErr = (adapters.Ollama{}).Run(context.Background(), endpoint, model, promptText, temperature, schemaText)
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
	command := ""
	var args []string
	for _, prop := range agent.Props {
		switch prop.Name {
		case "command":
			command, _ = ast.StringValue(prop.Value)
		case "args":
			var ok bool
			args, ok = ast.StringArrayValue(prop.Value)
			if !ok {
				return "", nil, fmt.Errorf("agent %q args must be an array of strings", agent.Name)
			}
		}
	}
	if command == "" {
		return "", nil, fmt.Errorf("agent %q has no command", agent.Name)
	}
	return command, args, nil
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
	for _, prop := range agent.Props {
		if prop.Name == "engine" {
			return ast.StringValue(prop.Value)
		}
	}
	return "", false
}

// agentLogPath reads an agent's `log` property — a file path that every
// cli/* engine call appends its subprocess's raw stdout to, creating the
// file (and any missing parent directories) on first write. ok is false
// when the agent declares no `log` property, letting callers skip the
// write entirely rather than treating "no log" and "empty path" the same
// way. Not consulted for the ollama/* engine, since that path calls an
// HTTP endpoint rather than a subprocess.
func agentLogPath(agent *ast.Agent) (string, bool) {
	for _, prop := range agent.Props {
		if prop.Name == "log" {
			path, ok := ast.StringValue(prop.Value)
			return path, ok && path != ""
		}
	}
	return "", false
}

// agentOllamaConfig reads the endpoint/temperature configuration for an
// ollama/* engine agent. model is derived from engine (the part after
// "ollama/"). temperature is nil when the agent declares no temperature
// property.
func agentOllamaConfig(agent *ast.Agent, engine string) (endpoint, model string, temperature *float64, err error) {
	model = strings.TrimPrefix(engine, "ollama/")
	for _, prop := range agent.Props {
		switch prop.Name {
		case "endpoint":
			endpoint, _ = ast.StringValue(prop.Value)
		case "temperature":
			t, ok := ast.NumberValue(prop.Value)
			if !ok {
				return "", "", nil, fmt.Errorf("agent %q temperature must be a number", agent.Name)
			}
			temperature = &t
		}
	}
	if endpoint == "" {
		return "", "", nil, fmt.Errorf("agent %q has no endpoint", agent.Name)
	}
	return endpoint, model, temperature, nil
}

// objectField looks up a field by name (string or bare-identifier key) in
// an already-unwrapped object literal, e.g. the `{ max_attempts: 3, ... }`
// value of a `retry:` property.
func objectField(obj *ast.Object, name string) (*ast.Expr, bool) {
	if obj == nil {
		return nil, false
	}
	for _, f := range obj.Fields {
		if (f.KeyIdent != nil && *f.KeyIdent == name) || (f.KeyStr != nil && *f.KeyStr == name) {
			return f.Value, true
		}
	}
	return nil, false
}

// agentRetry reads an agent's `retry: { max_attempts, delay, retry_on }`
// property into a traffic.Retrier. Absent `retry` yields a Retrier that
// runs the call exactly once, so callers can use it unconditionally.
//
// SKETCH GAP: `backoff` is accepted but ignored — traffic.Retrier only
// implements exponential backoff today, so a declared "linear"/"fixed"
// strategy would silently behave as exponential rather than erroring.
func agentRetry(agent *ast.Agent) (traffic.Retrier, error) {
	retrier := traffic.Retrier{MaxAttempts: 1, Delay: time.Second}
	for _, prop := range agent.Props {
		if prop.Name != "retry" {
			continue
		}
		obj := ast.BareObject(prop.Value)
		if obj == nil {
			return traffic.Retrier{}, fmt.Errorf("agent %q retry must be an object", agent.Name)
		}
		if v, ok := objectField(obj, "max_attempts"); ok {
			n, numOk := ast.NumberValue(v)
			if !numOk {
				return traffic.Retrier{}, fmt.Errorf("agent %q retry.max_attempts must be a number", agent.Name)
			}
			retrier.MaxAttempts = int(n)
		}
		if v, ok := objectField(obj, "delay"); ok {
			d, durOk := ast.DurationValue(v)
			if !durOk {
				return traffic.Retrier{}, fmt.Errorf("agent %q retry.delay must be a duration", agent.Name)
			}
			retrier.Delay = d
		}
		if v, ok := objectField(obj, "retry_on"); ok {
			codes, arrOk := stringOrNumberArray(v)
			if !arrOk {
				return traffic.Retrier{}, fmt.Errorf("agent %q retry.retry_on must be an array of strings/numbers", agent.Name)
			}
			retrier.RetryOn = codes
		}
		break
	}
	return retrier, nil
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
// SKETCH GAP: `strategy` is accepted but ignored — traffic.Cache only
// implements exact-match (SHA-256 of engine+prompt+parameters) today.
func agentCache(agent *ast.Agent) (cache *traffic.Cache, ttl time.Duration, hasCache bool, err error) {
	for _, prop := range agent.Props {
		if prop.Name != "cache" {
			continue
		}
		obj := ast.BareObject(prop.Value)
		if obj == nil {
			return nil, 0, false, fmt.Errorf("agent %q cache must be an object", agent.Name)
		}
		ttl = 24 * time.Hour
		if v, ok := objectField(obj, "ttl"); ok {
			d, durOk := ast.DurationValue(v)
			if !durOk {
				return nil, 0, false, fmt.Errorf("agent %q cache.ttl must be a duration", agent.Name)
			}
			ttl = d
		}
		dir := ""
		if v, ok := objectField(obj, "storage"); ok {
			storage, strOk := ast.StringValue(v)
			if !strOk {
				return nil, 0, false, fmt.Errorf("agent %q cache.storage must be a string", agent.Name)
			}
			if storage == "disk" {
				dir = ".mhl-cache"
			}
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
	return nil, 0, false, nil
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
// SKETCH GAP: `on_exceeded` is read and stored on the Limiter, but
// Limiter.Acquire only implements one behavior (block and wait) regardless
// of its value — a "reject" (fail-fast) mode isn't implemented.
func agentLimiter(agent *ast.Agent) (*traffic.Limiter, error) {
	for _, prop := range agent.Props {
		if prop.Name != "rate_limit" {
			continue
		}
		obj := ast.BareObject(prop.Value)
		if obj == nil {
			return nil, fmt.Errorf("agent %q rate_limit must be an object", agent.Name)
		}
		limiter := &traffic.Limiter{}
		if v, ok := objectField(obj, "requests_per_minute"); ok {
			n, numOk := ast.NumberValue(v)
			if !numOk {
				return nil, fmt.Errorf("agent %q rate_limit.requests_per_minute must be a number", agent.Name)
			}
			limiter.RequestsPerMinute = int(n)
		}
		if v, ok := objectField(obj, "concurrency"); ok {
			n, numOk := ast.NumberValue(v)
			if !numOk {
				return nil, fmt.Errorf("agent %q rate_limit.concurrency must be a number", agent.Name)
			}
			limiter.Concurrency = int(n)
		}
		if v, ok := objectField(obj, "on_exceeded"); ok {
			s, strOk := ast.StringValue(v)
			if !strOk {
				return nil, fmt.Errorf("agent %q rate_limit.on_exceeded must be a string", agent.Name)
			}
			limiter.OnExceeded = s
		}
		agentLimitersMu.Lock()
		shared, ok := agentLimiters[agent]
		if !ok {
			shared = limiter
			agentLimiters[agent] = shared
		}
		agentLimitersMu.Unlock()
		return shared, nil
	}
	return &traffic.Limiter{}, nil
}

// agentFallback reads an agent's `fallback: [...]` property. Each item is
// either an inline `agent { ... }` literal (Primary.Agent — the same node
// ast.Agent already reuses for a top-level `export agent Name {}`
// declaration, just with Name left blank), or a bare identifier naming
// another agent already declared in the program (e.g. `fallback:
// [ClaudeCLI]`), resolved the same way a top-level `ClaudeCLI.run(...)`
// statement resolves its receiver.
func agentFallback(prog *ast.Program, agent *ast.Agent) ([]*ast.Agent, error) {
	for _, prop := range agent.Props {
		if prop.Name != "fallback" {
			continue
		}
		arr := ast.BareArray(prop.Value)
		if arr == nil {
			return nil, fmt.Errorf("agent %q fallback must be an array", agent.Name)
		}
		fallbacks := make([]*ast.Agent, 0, len(arr.Items))
		for _, item := range arr.Items {
			if fb, ok := ast.AgentValue(item); ok {
				fallbacks = append(fallbacks, fb)
				continue
			}
			if name, ok := ast.IdentValue(item); ok {
				fb, found := findAgent(prog, name)
				if !found {
					return nil, fmt.Errorf("agent %q fallback: agent %q is not declared", agent.Name, name)
				}
				fallbacks = append(fallbacks, fb)
				continue
			}
			return nil, fmt.Errorf("agent %q fallback entries must be an inline agent {...} block or a declared agent name", agent.Name)
		}
		return fallbacks, nil
	}
	return nil, nil
}

// stringOrNumberArray reads an array literal whose elements may mix string
// and bare-number literals (e.g. `retry_on: [500, 503, "rate_limit"]`),
// stringifying numbers so both forms can be matched uniformly against an
// error message later (see traffic.Retrier.shouldRetry).
func stringOrNumberArray(e *ast.Expr) ([]string, bool) {
	arr := ast.BareArray(e)
	if arr == nil {
		return nil, false
	}
	values := make([]string, 0, len(arr.Items))
	for _, item := range arr.Items {
		if s, ok := ast.StringValue(item); ok {
			values = append(values, s)
			continue
		}
		if n, ok := ast.NumberValue(item); ok {
			values = append(values, strconv.FormatFloat(n, 'f', -1, 64))
			continue
		}
		return nil, false
	}
	return values, true
}
