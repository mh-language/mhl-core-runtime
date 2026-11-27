package lsp

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// propertyItem builds a plain `name: ` completion (matching methodItems'
// `m + "("` convention for its own InsertText) for a property that isn't a
// grammar keyword — checkpoint/repeat/retry/... are all just an ordinary
// `Ident ':' expr` Property node (internal/lang/ast), same as any agent
// config field, not a reserved word the parser itself knows about.
func propertyItem(name, detail string) completionItem {
	return completionItem{Label: name, Kind: kindProperty, Detail: detail, InsertText: name + ": "}
}

// completionTagDeprecated is LSP's CompletionItemTag.Deprecated.
const completionTagDeprecated = 1

// deprecatedItem marks it struck through and sorts it after the others.
func deprecatedItem(it completionItem) completionItem {
	it.Tags = []int{completionTagDeprecated}
	it.SortText = "~" + it.Label
	return it
}

// pipelinePropertyItems/loopPipelineExtraPropertyItems are what's valid
// directly inside a `pipeline { ... }` body beyond the Step/PipelineInput/
// VarDecl keywords already in the `keywords` list (input/var/step). Both are
// derived from ast.PipelineBodyProperties — the single source of truth also
// used by runtime.PipelineFromAST and lint — so a new body property is one
// edit in `ast`, never here. `repeat` (LoopOnly) is offered only under a
// `loop` pipeline, where it actually takes effect.
var pipelinePropertyItems, loopPipelineExtraPropertyItems = func() (base, loopOnly []completionItem) {
	for _, p := range ast.PipelineBodyProperties {
		if p.ParamType != "" {
			base = append(base, onHookItem(p))
		}
		it := propertyItem(p.Name, p.Doc)
		if p.Deprecated {
			it = deprecatedItem(it)
		}
		if p.LoopOnly {
			loopOnly = append(loopOnly, it)
		} else {
			base = append(base, it)
		}
	}
	return base, loopOnly
}()

// onHookItem is the `on <event>(param) { ... }` snippet for one lifecycle
// hook (ast.PipelineHook) — the current spelling; its `<event>: (param) ->`
// property item is offered struck through beside it.
func onHookItem(p ast.PipelineBodyProperty) completionItem {
	param := map[string]string{"SessionContext": "session", "StepContext": "step", "FailureContext": "failure"}[p.ParamType]
	return completionItem{
		Label:            "on " + p.Name,
		Kind:             kindSnippet,
		Detail:           p.Doc,
		InsertText:       "on " + p.Name + "(${1:" + param + "}) {\n\t$0\n}",
		InsertTextFormat: insertTextFormatSnippet,
	}
}

// spawnFieldItems mirrors runtime.spawnConfigFromExpr's field switch.
var spawnFieldItems = []completionItem{
	deprecatedItem(propertyItem("max_concurrency", "deprecated — prefer `spawn xs concurrency N = ...`; integer > 0, run-wide ceiling (default 4)")),
}

// checkpointFieldItems are the fields the deprecated `checkpoint: { ... }`
// block used to read. The runtime ignores them all now (every run is
// checkpointed per step, resumable for 30 days); they are still offered,
// struck through, so an existing block completes and explains itself.
var checkpointFieldItems = []completionItem{
	deprecatedItem(propertyItem("enabled", "ignored — checkpointing can no longer be turned off")),
	deprecatedItem(propertyItem("strategy", `ignored — always "per_step"`)),
	deprecatedItem(propertyItem("storage", "ignored")),
	deprecatedItem(propertyItem("ttl", "ignored — a stopped run stays resumable for 30 days")),
}

// repeatFieldItems mirrors runtime.repeatConfigFromExpr's field switch.
var repeatFieldItems = []completionItem{
	deprecatedItem(propertyItem("stop_when", "deprecated — end the loop with `break` in a step")),
	deprecatedItem(propertyItem("max_iterations", "deprecated — write `loop workflow X max N`")),
}

// contextFieldItems mirrors runtime.contextConfigFromExpr's field switch.
var contextFieldItems = []completionItem{
	deprecatedItem(propertyItem("source", `deprecated — "latest" (default) or "session:<id>": which prior run context.vars is read from`)),
	deprecatedItem(propertyItem("require", "deprecated — boolean: fail the run when source resolves to no stored state")),
}

// agentPropertyItems lists only what internal/engine/interpreter/agent.go
// actually reads (agentEngine/agentCommand/agentOllamaConfig/resolveAgentLog/resolveAgentCwd/
// agentTrace/agentRetry/agentCache/agentLimiter/agentFallback/agentHookExpr)
// — not every field docs/site/reference.html may still show aspirationally
// (api_key, system_instructions aren't implemented yet), so this
// list doesn't suggest config an agent declaration can write but the runtime
// will silently ignore.
var agentPropertyItems = []completionItem{
	propertyItem("engine", `e.g. "cli/claude-code", "ollama/qwen2.5-coder"`),
	propertyItem("command", "cli/* engine: the executable to run"),
	propertyItem("args", "cli/* engine: argv — an array (supports ${prompt}/${schema} placeholders as whole elements, ${name} of a declared input anywhere in an element, and ...expr to splice in a string array, [] inserting nothing), or any expression yielding a string array"),
	propertyItem("stdin", "cli/* engine: text written to the subprocess's standard input (supports ${prompt}/${schema} and ${name} of a declared input, substituted wherever they appear). Keeps large content out of argv — needed on Windows, where CreateProcess caps the whole command line at ~32,767 characters"),
	propertyItem("cwd", "cli/* engine: directory the process starts in — any string expression, evaluated per call; relative to mhl's working directory; must exist (the call fails before starting otherwise); \"\" = mhl's own"),
	propertyItem("endpoint", "ollama/* engine: server URL"),
	propertyItem("temperature", "ollama/* engine only"),
	propertyItem("log", "cli/* engine: file path every call's raw stdout streams into, line by line — a string (interpolated for ${...} spans, e.g. ${context.session_id}) or any expression evaluated per call; \"\" turns it off for that call"),
	propertyItem("log_mode", `"append" (default: every attempt adds to the file) or "truncate" (each attempt starts the file empty)`),
	propertyItem("trace", "boolean: echo calling/response lines to the console (default off)"),
	propertyItem("retry", "{ max_attempts, delay, retry_on, backoff }"),
	propertyItem("cache", "{ ttl, storage, strategy }"),
	propertyItem("rate_limit", "{ requests_per_minute, concurrency, on_exceeded }"),
	propertyItem("fallback", "array of inline agent {...} literals or declared agent names"),
	propertyItem("timeout", "duration (e.g. 30m), duration string or seconds: per-attempt deadline — on expiry the process tree gets SIGTERM, then SIGKILL after kill_grace, and the call fails with a `timeout` error retry_on can match"),
	propertyItem("kill_grace", "duration (default 10s): wait between SIGTERM and SIGKILL once timeout elapses; 0s kills at once (Windows always kills at once)"),
	propertyItem("skills", "array of declared skill names this agent may receive via run(skills: [...]) — requires system_prompt"),
	propertyItem("system_prompt", "(skills, prompt) -> string: composes the outgoing prompt when a call selects skills; skills is a list of {name, frontmatter, content}"),
	propertyItem("before", "() -> {...}: runs once before the prompt is built; its returned object's fields become ${...} bindings"),
	propertyItem("after", "() -> {...}: runs once on the final response (bound as result); a returned string replaces it"),
	propertyItem("description", "string: optional summary of what this agent is for; a router's decider folds it into the decision prompt alongside the agent's name"),
}

// agentInputItem is the `input name: type [= default]` declaration an agent
// body may carry alongside its properties (ast.Agent.Inputs) — a per-call
// parameter `.run(prompt: ..., name: value)` fills.
var agentInputItem = completionItem{
	Label:            "input",
	Kind:             kindKeyword,
	Detail:           "per-call parameter: `input name: type [= default]`, filled by `.run(prompt: ..., name: value)` and readable by args, cwd, stdin, log, timeout and the hooks",
	InsertText:       "input ${1:name}: ${2:string}",
	InsertTextFormat: insertTextFormatSnippet,
}

// routerPropertyItems is what's valid directly inside a `router { ... }`
// body, derived from ast.RouterBodyProperties — the single source of truth
// also used by lint (checkRouterProperties) — the same pattern
// pipelinePropertyItems uses, not hand-written like agentPropertyItems.
var routerPropertyItems = func() []completionItem {
	items := make([]completionItem, 0, len(ast.RouterBodyProperties))
	for _, p := range ast.RouterBodyProperties {
		items = append(items, propertyItem(p.Name, p.Doc))
	}
	return items
}()

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
func propertyItemsFor(path string, stack []blockRef) []completionItem {
	if len(stack) == 0 {
		return nil
	}
	top := stack[len(stack)-1]
	if top.Kind == blockExtension {
		return extensionPropertyItems(path, top.ExtKind, top.ExtProvider)
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
		items := make([]completionItem, 0, len(agentPropertyItems)+1)
		items = append(items, agentInputItem)
		return append(items, agentPropertyItems...)
	case blockRouter:
		return routerPropertyItems
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
