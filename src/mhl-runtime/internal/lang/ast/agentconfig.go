package ast

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// This file holds the AST-literal-reading (not runtime-value-checking) parts
// of an agent declaration's config properties, shared by internal/lang/lint
// (static checking) and internal/engine/interpreter (runtime, which wraps
// each of these into its own stateful type — traffic.Retrier/Cache/Limiter —
// see agent.go's agentRetry/agentCache/agentLimiter/agentFallback). These
// functions only ever read literals already present in the parse tree; they
// have no side effects and never execute anything.

// AgentCommand reads an agent's `command`/`args` properties.
func AgentCommand(agent *Agent) (command string, args []string, err error) {
	for _, prop := range agent.Props {
		switch prop.Name {
		case "command":
			command, _ = StringValue(prop.Value)
		case "args":
			var ok bool
			args, ok = StringArrayValue(prop.Value)
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

// AgentEngine reads the agent's engine property. ok is false when the
// property is absent or not a string.
func AgentEngine(agent *Agent) (string, bool) {
	for _, prop := range agent.Props {
		if prop.Name == "engine" {
			return StringValue(prop.Value)
		}
	}
	return "", false
}

// AgentOllamaConfig reads the endpoint/temperature configuration for an
// ollama/* engine agent. model is derived from engine (the part after
// "ollama/"). temperature is nil when the agent declares no temperature
// property.
func AgentOllamaConfig(agent *Agent, engine string) (endpoint, model string, temperature *float64, err error) {
	model = strings.TrimPrefix(engine, "ollama/")
	for _, prop := range agent.Props {
		switch prop.Name {
		case "endpoint":
			endpoint, _ = StringValue(prop.Value)
		case "temperature":
			t, ok := NumberValue(prop.Value)
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

// objectField looks up a field by name (string or bare-identifier key) in an
// already-unwrapped object literal, e.g. the `{ max_attempts: 3, ... }` value
// of a `retry:` property.
func objectField(obj *Object, name string) (*Expr, bool) {
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

// stringOrNumberArray reads an array literal whose elements may mix string
// and bare-number literals (e.g. `retry_on: [500, 503, "rate_limit"]`),
// stringifying numbers so both forms can be matched uniformly.
func stringOrNumberArray(e *Expr) ([]string, bool) {
	arr := BareArray(e)
	if arr == nil {
		return nil, false
	}
	values := make([]string, 0, len(arr.Items))
	for _, item := range arr.Items {
		if s, ok := StringValue(item); ok {
			values = append(values, s)
			continue
		}
		if n, ok := NumberValue(item); ok {
			values = append(values, strconv.FormatFloat(n, 'f', -1, 64))
			continue
		}
		return nil, false
	}
	return values, true
}

// implementedBackoff is the only `retry.backoff` value traffic.Retrier
// actually implements today.
const implementedBackoff = "exponential"

// AgentRetryConfig reads an agent's `retry: { max_attempts, delay, retry_on
// }` property. Absent `retry` yields the same defaults a Retrier that runs
// the call exactly once would use (maxAttempts=1, delay=1s), so callers can
// apply the result unconditionally.
//
// `backoff` is validated, not silently accepted: traffic.Retrier only
// implements exponential backoff today, so declaring any other value (e.g.
// `backoff: "linear"`) is rejected here rather than quietly running
// exponential anyway — a caller has no other way to discover the mismatch.
func AgentRetryConfig(agent *Agent) (maxAttempts int, delay time.Duration, retryOn []string, err error) {
	maxAttempts, delay = 1, time.Second
	for _, prop := range agent.Props {
		if prop.Name != "retry" {
			continue
		}
		obj := BareObject(prop.Value)
		if obj == nil {
			return 0, 0, nil, fmt.Errorf("agent %q retry must be an object", agent.Name)
		}
		if v, ok := objectField(obj, "max_attempts"); ok {
			n, numOk := NumberValue(v)
			if !numOk {
				return 0, 0, nil, fmt.Errorf("agent %q retry.max_attempts must be a number", agent.Name)
			}
			maxAttempts = int(n)
		}
		if v, ok := objectField(obj, "delay"); ok {
			d, durOk := DurationValue(v)
			if !durOk {
				return 0, 0, nil, fmt.Errorf("agent %q retry.delay must be a duration", agent.Name)
			}
			delay = d
		}
		if v, ok := objectField(obj, "retry_on"); ok {
			codes, arrOk := stringOrNumberArray(v)
			if !arrOk {
				return 0, 0, nil, fmt.Errorf("agent %q retry.retry_on must be an array of strings/numbers", agent.Name)
			}
			retryOn = codes
		}
		if v, ok := objectField(obj, "backoff"); ok {
			backoff, strOk := StringValue(v)
			if !strOk {
				return 0, 0, nil, fmt.Errorf("agent %q retry.backoff must be a string", agent.Name)
			}
			if backoff != implementedBackoff {
				return 0, 0, nil, fmt.Errorf("agent %q retry.backoff %q is not implemented — only %q is supported today", agent.Name, backoff, implementedBackoff)
			}
		}
		break
	}
	return maxAttempts, delay, retryOn, nil
}

// implementedCacheStrategy is the only `cache.strategy` value traffic.Cache
// actually implements today.
const implementedCacheStrategy = "exact"

// AgentCacheConfig reads an agent's `cache: { ttl, storage, strategy }`
// property. hasCache is false when the agent declares no `cache` property
// at all.
//
// `strategy` is validated, not silently accepted: traffic.Cache only
// implements exact-match caching (SHA-256 of engine+prompt+parameters)
// today, so declaring any other value (e.g. `strategy: "semantic"`) is
// rejected here rather than quietly falling back to exact-match anyway.
func AgentCacheConfig(agent *Agent) (ttl time.Duration, diskStorage bool, hasCache bool, err error) {
	for _, prop := range agent.Props {
		if prop.Name != "cache" {
			continue
		}
		obj := BareObject(prop.Value)
		if obj == nil {
			return 0, false, false, fmt.Errorf("agent %q cache must be an object", agent.Name)
		}
		ttl = 24 * time.Hour
		if v, ok := objectField(obj, "ttl"); ok {
			d, durOk := DurationValue(v)
			if !durOk {
				return 0, false, false, fmt.Errorf("agent %q cache.ttl must be a duration", agent.Name)
			}
			ttl = d
		}
		if v, ok := objectField(obj, "storage"); ok {
			storage, strOk := StringValue(v)
			if !strOk {
				return 0, false, false, fmt.Errorf("agent %q cache.storage must be a string", agent.Name)
			}
			diskStorage = storage == "disk"
		}
		if v, ok := objectField(obj, "strategy"); ok {
			strategy, strOk := StringValue(v)
			if !strOk {
				return 0, false, false, fmt.Errorf("agent %q cache.strategy must be a string", agent.Name)
			}
			if strategy != implementedCacheStrategy {
				return 0, false, false, fmt.Errorf("agent %q cache.strategy %q is not implemented — only %q is supported today", agent.Name, strategy, implementedCacheStrategy)
			}
		}
		return ttl, diskStorage, true, nil
	}
	return 0, false, false, nil
}

// implementedOnExceeded is the only `rate_limit.on_exceeded` value
// traffic.Limiter actually implements today.
const implementedOnExceeded = "queue"

// AgentLimiterConfig reads an agent's `rate_limit: { requests_per_minute,
// concurrency, on_exceeded }` property. hasLimit is false when the agent
// declares no `rate_limit` at all.
//
// `on_exceeded` is validated, not silently accepted: traffic.Limiter.Acquire
// only implements one behavior (block and wait, i.e. "queue") regardless of
// this value, so declaring anything else (e.g. `on_exceeded: "reject"`) is
// rejected here rather than quietly queuing anyway.
func AgentLimiterConfig(agent *Agent) (requestsPerMinute, concurrency int, onExceeded string, hasLimit bool, err error) {
	for _, prop := range agent.Props {
		if prop.Name != "rate_limit" {
			continue
		}
		obj := BareObject(prop.Value)
		if obj == nil {
			return 0, 0, "", false, fmt.Errorf("agent %q rate_limit must be an object", agent.Name)
		}
		if v, ok := objectField(obj, "requests_per_minute"); ok {
			n, numOk := NumberValue(v)
			if !numOk {
				return 0, 0, "", false, fmt.Errorf("agent %q rate_limit.requests_per_minute must be a number", agent.Name)
			}
			requestsPerMinute = int(n)
		}
		if v, ok := objectField(obj, "concurrency"); ok {
			n, numOk := NumberValue(v)
			if !numOk {
				return 0, 0, "", false, fmt.Errorf("agent %q rate_limit.concurrency must be a number", agent.Name)
			}
			concurrency = int(n)
		}
		if v, ok := objectField(obj, "on_exceeded"); ok {
			s, strOk := StringValue(v)
			if !strOk {
				return 0, 0, "", false, fmt.Errorf("agent %q rate_limit.on_exceeded must be a string", agent.Name)
			}
			if s != implementedOnExceeded {
				return 0, 0, "", false, fmt.Errorf("agent %q rate_limit.on_exceeded %q is not implemented — only %q is supported today", agent.Name, s, implementedOnExceeded)
			}
			onExceeded = s
		}
		return requestsPerMinute, concurrency, onExceeded, true, nil
	}
	return 0, 0, "", false, nil
}

// FallbackRef is one entry of `fallback: [...]`: either an inline agent{...}
// literal or a bare name referring to another declared agent. Resolving Name
// against the program (including import-alias handling) stays in each
// caller's own package — that resolution logic is itself already separately
// duplicated between internal/lang/lint and internal/engine/interpreter, a
// pre-existing duplication this type doesn't attempt to fix.
type FallbackRef struct {
	Inline *Agent
	Name   string
}

// AgentFallbackRefs reads an agent's `fallback: [...]` property. Each item is
// either an inline `agent { ... }` literal or a bare identifier naming
// another agent.
func AgentFallbackRefs(agent *Agent) ([]FallbackRef, error) {
	for _, prop := range agent.Props {
		if prop.Name != "fallback" {
			continue
		}
		arr := BareArray(prop.Value)
		if arr == nil {
			return nil, fmt.Errorf("agent %q fallback must be an array", agent.Name)
		}
		refs := make([]FallbackRef, 0, len(arr.Items))
		for _, item := range arr.Items {
			if fb, ok := AgentValue(item); ok {
				refs = append(refs, FallbackRef{Inline: fb})
				continue
			}
			if name, ok := IdentValue(item); ok {
				refs = append(refs, FallbackRef{Name: name})
				continue
			}
			return nil, fmt.Errorf("agent %q fallback entries must be an inline agent {...} block or a declared agent name", agent.Name)
		}
		return refs, nil
	}
	return nil, nil
}
