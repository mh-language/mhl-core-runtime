package lsp

// propertyItem builds a plain `name: ` completion (matching methodItems'
// `m + "("` convention for its own InsertText) for a property that isn't a
// grammar keyword — checkpoint/repeat/retry/... are all just an ordinary
// `Ident ':' expr` Property node (internal/lang/ast), same as any agent
// config field, not a reserved word the parser itself knows about.
func propertyItem(name, detail string) completionItem {
	return completionItem{Label: name, Kind: kindProperty, Detail: detail, InsertText: name + ": "}
}

// pipelinePropertyItems/loopPipelineExtraPropertyItems are what's valid
// directly inside a `pipeline { ... }` body beyond the Step/PipelineInput/
// VarDecl keywords already in the `keywords` list (input/var/step) —
// mirrors runtime.PipelineFromAST's own Prop.Name switch. `repeat` only
// makes sense on a `loop pipeline` (a plain pipeline runs once regardless of
// what it declares), so it's offered as an addition on top of the shared
// list rather than folded into it.
var pipelinePropertyItems = []completionItem{
	propertyItem("checkpoint", "{ enabled, strategy, storage, ttl }"),
}

var loopPipelineExtraPropertyItems = []completionItem{
	propertyItem("repeat", "{ stop_when, max_iterations }"),
}

// checkpointFieldItems mirrors runtime.checkpointFromExpr's field switch.
var checkpointFieldItems = []completionItem{
	propertyItem("enabled", "boolean"),
	propertyItem("strategy", `e.g. "per_step"`),
	propertyItem("storage", `e.g. "file"`),
	propertyItem("ttl", "duration, e.g. 7d"),
}

// repeatFieldItems mirrors runtime.repeatConfigFromExpr's field switch.
var repeatFieldItems = []completionItem{
	propertyItem("stop_when", "condition expression, re-evaluated after every iteration"),
	propertyItem("max_iterations", "integer ceiling"),
}

// agentPropertyItems lists only what internal/engine/interpreter/agent.go
// actually reads (agentEngine/agentCommand/agentOllamaConfig/agentLogPath/
// agentTrace/agentRetry/agentCache/agentLimiter/agentFallback) — not every
// field language-design.md's aspirational examples show (api_key, skills,
// mcp_servers, tools, timeout, system_instructions aren't implemented yet),
// so this list doesn't suggest config an agent declaration can write but
// the runtime will silently ignore.
var agentPropertyItems = []completionItem{
	propertyItem("engine", `e.g. "cli/claude-code", "ollama/qwen2.5-coder"`),
	propertyItem("command", "cli/* engine: the executable to run"),
	propertyItem("args", "cli/* engine: argv (supports ${prompt}/${schema} placeholders)"),
	propertyItem("endpoint", "ollama/* engine: server URL"),
	propertyItem("temperature", "ollama/* engine only"),
	propertyItem("log", "cli/* engine: file path every call's raw stdout is appended to"),
	propertyItem("trace", "boolean: echo calling/response lines to the console (default off)"),
	propertyItem("retry", "{ max_attempts, delay, retry_on, backoff }"),
	propertyItem("cache", "{ ttl, storage, strategy }"),
	propertyItem("rate_limit", "{ requests_per_minute, concurrency, on_exceeded }"),
	propertyItem("fallback", "array of inline agent {...} literals or declared agent names"),
}

// retryFieldItems mirrors agentRetry's field switch. backoff is listed even
// though agentRetry's own SKETCH GAP comment notes it's accepted but always
// exponential today — it's valid, non-erroring syntax, just not fully
// honored yet.
var retryFieldItems = []completionItem{
	propertyItem("max_attempts", "integer"),
	propertyItem("delay", "duration between attempts"),
	propertyItem("retry_on", "array of status codes/strings to retry on"),
	propertyItem("backoff", `accepted; only "exponential" is actually implemented today`),
}

// cacheFieldItems mirrors agentCache's field switch. strategy is listed for
// the same accepted-but-not-fully-honored reason as retry's backoff above.
var cacheFieldItems = []completionItem{
	propertyItem("ttl", "duration, default 24h"),
	propertyItem("storage", `"disk" persists across separate mhl runs; default is in-memory only`),
	propertyItem("strategy", "accepted; only exact-match is actually implemented today"),
}

// rateLimitFieldItems mirrors agentLimiter's field switch.
var rateLimitFieldItems = []completionItem{
	propertyItem("requests_per_minute", "integer"),
	propertyItem("concurrency", "integer: max simultaneous calls"),
	propertyItem("on_exceeded", `accepted; Limiter only actually blocks-and-waits today`),
}

// propertyItemsFor returns the property-name completions valid directly
// inside the block stack's innermost (last) entry — nil when that block
// isn't one property-position completion has an opinion about (a step body,
// an if/while/try block, a plain object literal, ...), in which case
// completionAt's general keyword+symbol list is left untouched.
func propertyItemsFor(stack []blockKind) []completionItem {
	if len(stack) == 0 {
		return nil
	}
	switch stack[len(stack)-1] {
	case blockPipeline:
		return pipelinePropertyItems
	case blockLoopPipeline:
		items := make([]completionItem, 0, len(pipelinePropertyItems)+len(loopPipelineExtraPropertyItems))
		items = append(items, pipelinePropertyItems...)
		items = append(items, loopPipelineExtraPropertyItems...)
		return items
	case blockAgent:
		return agentPropertyItems
	case blockCheckpoint:
		return checkpointFieldItems
	case blockRepeat:
		return repeatFieldItems
	case blockRetry:
		return retryFieldItems
	case blockCache:
		return cacheFieldItems
	case blockRateLimit:
		return rateLimitFieldItems
	default:
		return nil
	}
}
