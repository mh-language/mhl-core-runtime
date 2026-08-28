# agents

Declaring and calling `agent`s: how `.run(prompt: ...)` builds argv for a CLI-backed agent,
how a raw API is reached by wrapping `curl` in a `sh -c` subprocess, retry/fallback config,
and parsing a structured response with `json.parse`. Shared agent declarations live in
[fixtures/agents.mh](fixtures/agents.mh); each example below `use`s only the ones it needs.

Examples marked "calls the real api" are skipped unless `ENABLE_LLM_CALL=1` is set in the
environment — they need real network access and credentials, not just `mhl test`.

- [claude_cli_places_prompt_after_dash_p_flag.mh](claude_cli_places_prompt_after_dash_p_flag.mh)
  — a mocked CLI agent (real command swapped for `echo`) proves the exact argv mhl builds
- [claude_cli_calls_the_real_api.mh](claude_cli_calls_the_real_api.mh) — invoking the real
  `claude` CLI agent (gated behind `ENABLE_LLM_CALL`)
- [json_parse_extracts_structured_output_message.mh](json_parse_extracts_structured_output_message.mh)
  — parsing a CLI agent's structured JSON response with `json.parse`
- [gemini_api_calls_the_real_api.mh](gemini_api_calls_the_real_api.mh) — an HTTP API reached
  by wrapping `curl` in `sh -c`, with `retry`/`fallback` configured (gated behind
  `ENABLE_LLM_CALL`)
- [gemini_structured_api_splices_schema_as_a_json_object.mh](gemini_structured_api_splices_schema_as_a_json_object.mh)
  — a mocked structured-output agent proves `schema:` lands as a real JSON object, not a
  string
- [gemini_structured_api_calls_the_real_api.mh](gemini_structured_api_calls_the_real_api.mh)
  — the real structured-output Gemini call (gated behind `ENABLE_LLM_CALL`)
- [ollama_cli_calls_the_real_api.mh](ollama_cli_calls_the_real_api.mh) — an `ollama/*` engine
  agent talking to a local Ollama server (gated behind `ENABLE_LLM_CALL`)
- [agent_retry_cache_rate_limit_reject_unimplemented_values.mh](agent_retry_cache_rate_limit_reject_unimplemented_values.mh)
  — `retry.backoff`, `cache.strategy`, and `rate_limit.on_exceeded` each implement exactly one
  value today; declaring any other is a build-time error, not a silently-ignored one
- [agent_tools_and_mcp_servers_fold_into_the_prompt.mh](agent_tools_and_mcp_servers_fold_into_the_prompt.mh)
  — an agent's own `tools:`/`mcp_servers:` properties shape every `.run()` call with an explicit
  allowed-scope instruction (best-effort only — see `sample/features/mcp/` for the mcp_server
  side of this)
- [agent_before_after_hooks_fetch_real_data_for_the_prompt.mh](agent_before_after_hooks_fetch_real_data_for_the_prompt.mh)
  — `before: (mcp, tools) -> {...}` calls the agent's declared mcp_server/tool for real before the
  prompt is built, feeding its result into `${...}` interpolation; `after: (mcp, tools, result) ->
  {...}` runs once the response is in. Unlike `tools:`/`mcp_servers:`'s prompt text, this is real
  data mhl fetched itself, not a hint the model may ignore
- [agent_before_hook_navigates_multiple_tools_and_mcp_servers.mh](agent_before_hook_navigates_multiple_tools_and_mcp_servers.mh)
  — `mcp`/`tools` are maps keyed by declared name, so a `before` hook can reach every entry in
  `tools:`/`mcp_servers:`, not just the first
- [agent_before_hook_scopes_tool_access_by_method.mh](agent_before_hook_scopes_tool_access_by_method.mh)
  — a dotted `tools:` entry (`execution.read_file`) narrows a hook's `tools.execution` binding to
  exactly that method; naming the same tool bare anywhere in the list removes the restriction
