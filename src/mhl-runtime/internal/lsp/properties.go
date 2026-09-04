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
	propertyItem("description", `"..." — human-readable summary; surfaced as the MCP tool / A2A skill description by "mhl serve"`),
	propertyItem("checkpoint", "{ enabled, strategy, storage, ttl } — optional; per-step checkpointing is on by default, declare enabled: false to opt out"),
	propertyItem("spawn", "{ max_concurrency } — run-wide ceiling on concurrent spawned agent calls"),
	propertyItem("context", "{ source, require } — populate context.vars from a prior run (context.session_id / .started_at / .resumed / .principal need no block)"),
}

// spawnFieldItems mirrors runtime.spawnConfigFromExpr's field switch.
var spawnFieldItems = []completionItem{
	propertyItem("max_concurrency", "integer > 0; default 4"),
}

var loopPipelineExtraPropertyItems = []completionItem{
	propertyItem("repeat", "{ stop_when, max_iterations } — the `max <N>` header clause is shorthand for just max_iterations"),
}

// checkpointFieldItems mirrors runtime.checkpointFromExpr's field switch.
var checkpointFieldItems = []completionItem{
	propertyItem("enabled", "boolean; default true — set false to disable checkpointing"),
	propertyItem("strategy", `e.g. "per_step" (the default)`),
	propertyItem("storage", `e.g. "file"`),
	propertyItem("ttl", "duration, e.g. 7d"),
}

// repeatFieldItems mirrors runtime.repeatConfigFromExpr's field switch.
var repeatFieldItems = []completionItem{
	propertyItem("stop_when", "condition expression, re-evaluated after every iteration"),
	propertyItem("max_iterations", "integer ceiling"),
}

// contextFieldItems mirrors runtime.contextConfigFromExpr's field switch.
var contextFieldItems = []completionItem{
	propertyItem("source", `"latest" (default) or "session:<id>" — which prior run context.vars is read from`),
	propertyItem("require", "boolean: fail the run when source resolves to no stored state"),
}

// agentPropertyItems lists only what internal/engine/interpreter/agent.go
// actually reads (agentEngine/agentCommand/agentOllamaConfig/agentLogPath/
// agentTrace/agentRetry/agentCache/agentLimiter/agentFallback/agentHookExpr)
// — not every field docs/site/reference.html may still show aspirationally
// (api_key, timeout, system_instructions aren't implemented yet), so this
// list doesn't suggest config an agent declaration can write but the runtime
// will silently ignore.
var agentPropertyItems = []completionItem{
	propertyItem("engine", `e.g. "cli/claude-code", "ollama/qwen2.5-coder"`),
	propertyItem("command", "cli/* engine: the executable to run"),
	propertyItem("args", "cli/* engine: argv (supports ${prompt}/${schema} placeholders)"),
	propertyItem("endpoint", "ollama/* engine: server URL"),
	propertyItem("temperature", "ollama/* engine only"),
	propertyItem("log", "cli/* engine: file path every call's raw stdout is appended to (interpolated for ${...} spans, e.g. ${context.session_id}, so each run gets its own file)"),
	propertyItem("trace", "boolean: echo calling/response lines to the console (default off)"),
	propertyItem("retry", "{ max_attempts, delay, retry_on, backoff }"),
	propertyItem("cache", "{ ttl, storage, strategy }"),
	propertyItem("rate_limit", "{ requests_per_minute, concurrency, on_exceeded }"),
	propertyItem("fallback", "array of inline agent {...} literals or declared agent names"),
	propertyItem("before", "() -> {...}: runs once before the prompt is built; its returned object's fields become ${...} bindings"),
	propertyItem("after", "() -> {...}: runs once on the final response (bound as result); a returned string replaces it"),
}

// retryFieldItems mirrors agentRetry's field switch. backoff is listed even
// though ast.AgentRetryConfig rejects any value other than "exponential" —
// it's valid syntax, just constrained to the one implemented value.
var retryFieldItems = []completionItem{
	propertyItem("max_attempts", "integer"),
	propertyItem("delay", "duration between attempts"),
	propertyItem("retry_on", "array of status codes/strings to retry on"),
	propertyItem("backoff", `only "exponential" is implemented; any other value is rejected`),
}

// cacheFieldItems mirrors agentCache's field switch. strategy is listed for
// the same implemented-value-only reason as retry's backoff above.
var cacheFieldItems = []completionItem{
	propertyItem("ttl", "duration, default 24h"),
	propertyItem("storage", `"disk" persists across separate mhl runs; default is in-memory only`),
	propertyItem("strategy", `only "exact" is implemented; any other value is rejected`),
}

// rateLimitFieldItems mirrors agentLimiter's field switch.
var rateLimitFieldItems = []completionItem{
	propertyItem("requests_per_minute", "integer"),
	propertyItem("concurrency", "integer: max simultaneous calls"),
	propertyItem("on_exceeded", `only "queue" (block-and-wait) is implemented; any other value is rejected`),
}

// propertyItemsFor returns the property-name completions valid directly
// inside the block stack's innermost (last) entry — nil when that block
// isn't one property-position completion has an opinion about (a step body,
// an if/while/try block, a plain object literal, ...), in which case
// completionAt's general keyword+symbol list is left untouched.
func propertyItemsFor(stack []blockRef) []completionItem {
	if len(stack) == 0 {
		return nil
	}
	top := stack[len(stack)-1]
	if top.Kind == blockExtension {
		return extensionPropertyItems(top.ExtKind)
	}
	switch top.Kind {
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
	case blockSpawn:
		return spawnFieldItems
	case blockRepeat:
		return repeatFieldItems
	case blockContext:
		return contextFieldItems
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
