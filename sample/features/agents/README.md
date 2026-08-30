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
- [agent_before_after_hooks_fetch_real_data_for_the_prompt.mh](agent_before_after_hooks_fetch_real_data_for_the_prompt.mh)
  — `before: () -> {...}` calls declared tools/extensions by name for real before the prompt is
  built, feeding the result into `${...}` interpolation; `after: () -> {...}` runs once the
  response is in, with it bound as `result`. These are real calls mhl executes itself — an agent
  is a single-shot CLI process and cannot call a tool or extension mid-generation on its own
- [agent_before_hook_fetches_from_multiple_tools_and_extensions.mh](agent_before_hook_fetches_from_multiple_tools_and_extensions.mh)
  — a `before` hook body is an ordinary expression scope, so it can call any number of declared
  tools and extensions
- [agent_log_path_interpolates_per_run.mh](agent_log_path_interpolates_per_run.mh)
  — an agent's `log:` path is interpolated for `${...}` spans against the calling scope, so
  `${context.session_id}` (or any in-scope var) gives each run its own file instead of every
  concurrent run of one pipeline appending into a single shared log
