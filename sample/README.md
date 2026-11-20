# sample — dicionário de A a Z

Índice de todos os recursos da linguagem **mhl** (`.mh`) demonstrados nesta pasta. Cada
verbete aponta para o README do tópico e/ou para o arquivo `.mh` que o exercita.

A pasta tem duas metades:

- [syntax/](syntax/README.md) — expressões e instruções (o que se escreve dentro de um `test`,
  de um método de `tool` ou de um `step`). Todo arquivo é um `mhl test` autoverificável.
- [features/](features/README.md) — as declarações de alto nível (agents, memory, prompts,
  pipelines, extensions...) e os namespaces nativos (`fs`, `git`, `http`, `time`...).

Para rodar tudo: `make functional-test` (a partir de `src/mhl-runtime`), ou um tópico só:
`mhl test sample/syntax/<tópico>` / `mhl run sample/features/pipelines/<arquivo>.mh`.

A referência normativa da linguagem é [docs/site/Docs-Reference.dc.html](../docs/site/Docs-Reference.dc.html);
este índice cobre apenas o que tem exemplo executável aqui.

---

[A](#a) · [B](#b) · [C](#c) · [D](#d) · [E](#e) · [F](#f) · [G](#g) · [H](#h) · [I](#i) ·
[J](#j) · [K](#k) · [L](#l) · [M](#m) · [N](#n) · [O](#o) · [P](#p) · [Q](#q) · [R](#r) ·
[S](#s) · [T](#t) · [U](#u) · [V](#v) · [W](#w) · [X](#x)

---

## A

- **`a2a`** (extension) — cliente de agentes remotos A2A: `.send()`, `.agent_card()`,
  `.get_task()`, `.cancel()`. → [features/a2a/](features/a2a/README.md)
- **`after`** (hook de agent) — roda sobre a resposta final, com ela ligada a `result`.
  → [agent_before_after_hooks_fetch_real_data_for_the_prompt.mh](features/agents/agent_before_after_hooks_fetch_real_data_for_the_prompt.mh)
- **`agent`** — declaração de um modelo/CLI chamado com `.run(prompt: ...)`.
  → [features/agents/](features/agents/README.md) · [fixtures/agents.mh](features/agents/fixtures/agents.mh)
- **`.all()` / `.any()`** — quantificadores universal/existencial sobre arrays.
  → [any_and_all.mh](syntax/higher_order_array/any_and_all.mh)
- **`.append()`** — novo array com o elemento ao final; como instrução, é açúcar para
  `xs = xs.append(x)`. → [append_returns_a_new_array.mh](syntax/array_collection_ops/append_returns_a_new_array.mh) ·
  [append_statement_grows_the_variable.mh](syntax/array_collection_ops/append_statement_grows_the_variable.mh)
- **`args`** (agent) — argv do processo; aceita spread `...xs` e placeholders `"${name}"`.
  → [agent_takes_inputs_and_spreads_args.mh](features/agents/agent_takes_inputs_and_spreads_args.mh) ·
  [claude_cli_places_prompt_after_dash_p_flag.mh](features/agents/claude_cli_places_prompt_after_dash_p_flag.mh)
- **Aritmética** (`+ - * / %`) — o tipo único `number`. → [syntax/math_operations/](syntax/math_operations/README.md)
- **Arrays** — literais, indexação, mutação, concatenação, arrays aninhados e mistos.
  → [syntax/array_statements/](syntax/array_statements/README.md)
- **Asserções** (`are_equal`, `are_not_equal`, `is_true`, `is_false`, `is_null`, `not_null`,
  `includes`) — usadas dentro de `test`/`describe`. → ver [`test`](#t)
- **Atribuição composta `+=`** — números, strings e arrays; também em índices.
  → [syntax/compound_assignment/](syntax/compound_assignment/README.md)

## B

- **`before`** (hook de agent) — roda antes do prompt; chama `tool`s/`extension`s e expõe os
  campos retornados como `${...}`. → [agent_before_after_hooks_fetch_real_data_for_the_prompt.mh](features/agents/agent_before_after_hooks_fetch_real_data_for_the_prompt.mh) ·
  [agent_before_hook_fetches_from_multiple_tools_and_extensions.mh](features/agents/agent_before_hook_fetches_from_multiple_tools_and_extensions.mh)
- **`branches`** (parâmetro de `merge`) — `branches.<Step>.<var>` com o que cada ramo deixou.
  → [parallel_merge_workflow_example.mh](features/pipelines/parallel_merge_workflow_example.mh)
- **`break`** — encerra o run de forma limpa (não é falha).
  → [run_workflow_reports_break.mh](features/test_run_workflow/run_workflow_reports_break.mh) ·
  [order_triage.mh](features/graph_and_events/order_triage.mh)

## C

- **`cache`** (agent) — cache de respostas (`strategy: "exact"`).
  → [agent_retry_cache_rate_limit_reject_unimplemented_values.mh](features/agents/agent_retry_cache_rate_limit_reject_unimplemented_values.mh)
- **`checkpoint`** — persistência por step para `--resume` (ligada por padrão; `ttl`, opt-out).
  → [context_pipeline_example.mh](features/pipelines/context_pipeline_example.mh) ·
  [linear_pipeline_and_branching_workflow.mh](syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh) ·
  [time_checkpoint_expiry_uses_add_and_compare.mh](features/time/time_checkpoint_expiry_uses_add_and_compare.mh)
- **`cli/*`** (engine de agent) — agente apoiado em um CLI local (ex.: `cli/claude-code`).
  → [claude_cli_calls_the_real_api.mh](features/agents/claude_cli_calls_the_real_api.mh)
- **`cmd.exec`** — executa um processo com argv seguro, retorna `{stdout, stderr, exit_code}`.
  → [memory_session_mem_value_survives_cmd_exec_argv.mh](features/memory/memory_session_mem_value_survives_cmd_exec_argv.mh)
- **`cmd.exec(..., cwd:)`** — executa o comando em outro diretório (`cd cwd && comando`).
  → [cmd_exec_cwd_runs_in_another_directory.mh](features/cmd/cmd_exec_cwd_runs_in_another_directory.mh)
- **Comparação** (`< > <= >= == !=`). → [syntax/conditional_symbols/](syntax/conditional_symbols/README.md)
- **`complete()`** — termina o workflow com sucesso a partir de qualquer step.
  → [order_triage.mh](features/graph_and_events/order_triage.mh) ·
  [goto_parallel_group.mh](syntax/pipeline_vs_workflow/goto_parallel_group.mh)
- **Concatenação** — `+` entre strings (e string + escalar). → [syntax/concatenation/](syntax/concatenation/README.md)
- **`const`** — ligação de atribuição única; também no topo de pipelines e dentro de `tool`s.
  → [syntax/const_declaration/](syntax/const_declaration/README.md) ·
  [const_is_available_to_every_method.mh](syntax/tool_scope/const_is_available_to_every_method.mh)
- **`.contains()`** — em strings e arrays. → [contains_on_string.mh](syntax/string_methods/contains_on_string.mh) ·
  [contains_on_array.mh](syntax/string_methods/contains_on_array.mh)
- **`context`** — acessor somente leitura (`session_id`, `started_at`, `resumed`, `vars`).
  → [context_pipeline_example.mh](features/pipelines/context_pipeline_example.mh)
- **`cwd`** (agent) — diretório de trabalho do processo, avaliado por chamada.
  → [agent_runs_a_cli_without_a_wrapper.mh](features/agents/agent_runs_a_cli_without_a_wrapper.mh)

## D

- **`decider`** (router) — agent comum que decide quando `select` é inconclusivo.
  → [router_cascades_to_llm_decision_when_select_is_inconclusive.mh](features/router/router_cascades_to_llm_decision_when_select_is_inconclusive.mh)
- **`.delegate()`** (router) — escolhe e executa o agent. → [features/router/](features/router/README.md)
- **`description:`** — descrição de um pipeline/workflow (vira a descrição da tool MCP).
  → [order_triage.mh](features/graph_and_events/order_triage.mh)
- **Desestruturação** — `var {a, b: n} = r`, `{a} = r`, `self.{a} = r`.
  → [syntax/destructuring/](syntax/destructuring/README.md)
- **`dir.clear`** — cria ou esvazia um diretório. → [dir_clear_empties_a_directory.mh](features/fs/dir_clear_empties_a_directory.mh)
- **`.discover()`** (mcp) — `server/discover`. → [stdio_mock_server_proves_discover_wiring.mh](features/mcp/stdio_mock_server_proves_discover_wiring.mh)
- **Durações** (`2ms`, `10m`, `7d`) — literais aceitos por `timeout`, `time.add`, `time.sleep`, `ttl`.
  → [time_add_duration_literal.mh](features/time/time_add_duration_literal.mh)

## E

- **`engine`** (agent) — `cli/*` ou `ollama/*`. → [fixtures/agents.mh](features/agents/fixtures/agents.mh)
- **`entry route`** — um `route` que inicia o run: despacha pelo input de mesmo nome antes de
  qualquer step. → [entry_route_dispatches_on_an_input.mh](syntax/pipeline_vs_workflow/entry_route_dispatches_on_an_input.mh)
- **`enum`** — conjunto fechado de constantes; `Kind.parse()`, `Kind.values()`.
  → [syntax/enum_and_match/](syntax/enum_and_match/README.md) ·
  [run_workflow_enum_input.mh](features/test_run_workflow/run_workflow_enum_input.mh)
- **`.enumerate()`** — pares `[índice, valor]`. → [enumerate_pairs_index_with_value.mh](syntax/array_collection_ops/enumerate_pairs_index_with_value.mh)
- **`env()`** — variável de ambiente (1 argumento: obrigatória e redigida; 2: com default).
  → [agent_command_env_with_declarative_fallback.mh](features/agents/agent_command_env_with_declarative_fallback.mh)
- **`.equals()` / `.deep_equal()`** — comparação profunda de valores.
  → [equals_is_a_deep_comparison.mh](syntax/array_collection_ops/equals_is_a_deep_comparison.mh)
- **`extension`** — `extension <kind> <Name> [from "<id>"] { ... }`.
  → [syntax/extension_declaration/](syntax/extension_declaration/README.md) ·
  [features/mcp/](features/mcp/README.md) · [features/a2a/](features/a2a/README.md)
- **`.extract_content()`** — mantém só o trecho entre dois marcadores.
  → [extract_content.mh](syntax/string_methods/extract_content.mh)

## F

- **`fail()`** — falha o step com uma razão (capturável em teste via `.run()`).
  → [run_workflow_reports_step_failure.mh](features/test_run_workflow/run_workflow_reports_step_failure.mh)
- **`fallback`** (agent) — agent alternativo quando as tentativas se esgotam.
  → [gemini_api_calls_the_real_api.mh](features/agents/gemini_api_calls_the_real_api.mh) ·
  [agent_timeout_stops_a_hung_process.mh](features/agents/agent_timeout_stops_a_hung_process.mh)
- **`.filter()` / `.find()`** — seleção por predicado. → [syntax/higher_order_array/](syntax/higher_order_array/README.md)
- **`finally`** — ver [`try`](#t).
- **`flat_map`** — mapeia e achata. → [agent_takes_inputs_and_spreads_args.mh](features/agents/agent_takes_inputs_and_spreads_args.mh)
- **`for (var x in xs)`** — laço sobre arrays. → [syntax/looping_constructs/](syntax/looping_constructs/README.md)
- **`from "<arquivo>"`** — carrega o corpo de fora: `prompt ... from`, `schema ... from`,
  `extension ... from`. → [prompt_loaded_from_markdown_file.mh](features/prompts/prompt_loaded_from_markdown_file.mh) ·
  [schema_declaration.mh](features/schemas/schema_declaration.mh) ·
  [extension_from_names_the_provider.mh](syntax/extension_declaration/extension_from_names_the_provider.mh)
- **`fs.*`** — `fs.read`, `fs.write`, `fs.append`, `fs.exists`, com `encoding`.
  → [features/fs/](features/fs/README.md) ·
  [memory_session_mem_stringify_roundtrips_through_a_file.mh](features/memory/memory_session_mem_stringify_roundtrips_through_a_file.mh)

## G

- **`.get(key, default)`** — leitura de objeto sem exceção. → [object_get_with_default.mh](syntax/object_property_access/object_get_with_default.mh)
- **`git.*`** — `status`, `diff`, `log`, `rev_parse`, `add`, `commit`, com `dir:`.
  → [features/git/](features/git/README.md)
- **`goto`** — salto entre steps (só em `workflow`); `goto nameof(X)`; alvo pode ser um grupo `parallel`.
  → [syntax/pipeline_vs_workflow/](syntax/pipeline_vs_workflow/README.md) ·
  [goto_nameof_target.mh](syntax/pipeline_vs_workflow/goto_nameof_target.mh) ·
  [goto_parallel_group.mh](syntax/pipeline_vs_workflow/goto_parallel_group.mh)
- **`goto match`** — despacho estático de step por padrão.
  → [goto_match_artifact_review.mh](syntax/pipeline_vs_workflow/goto_match_artifact_review.mh)
- **`graph`** (`mhl graph`) — o workflow como grafo JSON com spans.
  → [features/graph_and_events/](features/graph_and_events/README.md)

## H

- **`html.*`** — `parse`, `get_element(s)`, `get_element_by_id`, `get_attribute`, `get_text`,
  `to_html`, `escape`, `attr_escape`. → [features/html/](features/html/README.md)
- **`http.*`** — um op por verbo + `download`; `query`, `body`/`text`/`form`, `auth`, `tls`, `proxy`.
  → [features/http/](features/http/README.md)

## I

- **`if` / `else`** — como expressão e como bloco. → [syntax/conditional_statements/](syntax/conditional_statements/README.md)
- **`import { X } from "arquivo.mh"`** — reutiliza declarações de outro arquivo.
  → [memory_session_mem.mh](features/memory/memory_session_mem.mh)
- **`.index_of()`** — posição ou `-1`. → [array_index_of_not_found.mh](syntax/array_statements/array_index_of_not_found.mh)
- **`input`** — entradas tipadas de pipeline (com default) e de agent (por chamada).
  → [agent_pipeline_example.mh](features/pipelines/agent_pipeline_example.mh) ·
  [run_workflow_applies_input_default.mh](features/test_run_workflow/run_workflow_applies_input_default.mh) ·
  [run_workflow_inputs_are_read_only.mh](features/test_run_workflow/run_workflow_inputs_are_read_only.mh) ·
  [agent_takes_inputs_and_spreads_args.mh](features/agents/agent_takes_inputs_and_spreads_args.mh)
- **`internal`** — método privado de `tool`; `internal workflow` (não é ponto de entrada).
  → [internal_method.mh](syntax/tool_scope/internal_method.mh) ·
  [run_workflow_alias.mh](features/test_run_workflow/run_workflow_alias.mh)
- **Interpolação `${...}`** — em strings, inclusive com literais aninhados.
  → [concatenation.mh](syntax/concatenation/concatenation.mh) ·
  [interpolation_with_a_nested_string_literal.mh](syntax/string_interpolation/interpolation_with_a_nested_string_literal.mh) ·
  [prompt_inline_interpolated_string.mh](features/prompts/prompt_inline_interpolated_string.mh)
- **`is_*`** (`is_string`, `is_number`, `is_bool`, `is_array`, `is_object`, `is_null`, `is_enum`).
  → [is_checks.mh](syntax/type_introspection/is_checks.mh)

## J

- **`.join()`** — array → string. → [join_formats_each_element.mh](syntax/array_collection_ops/join_formats_each_element.mh)
- **`json.parse` / `json.stringify`**.
  → [json_parse_extracts_structured_output_message.mh](features/agents/json_parse_extracts_structured_output_message.mh) ·
  [memory_session_mem_stringify_roundtrips_through_a_file.mh](features/memory/memory_session_mem_stringify_roundtrips_through_a_file.mh)
- **`jsonl`** (`mhl run --format jsonl`) — o run como fluxo de eventos.
  → [features/graph_and_events/](features/graph_and_events/README.md)

## K

- **`.keys()`** — chaves de um objeto. → [object_get_keys_and_values.mh](syntax/object_property_access/object_get_keys_and_values.mh)
- **`kill_grace`** (agent) — tempo entre SIGTERM e SIGKILL no timeout.
  → [agent_timeout_stops_a_hung_process.mh](features/agents/agent_timeout_stops_a_hung_process.mh)

## L

- **Lambdas** `(x) -> expr` — argumentos de `.map()`, `.filter()`, hooks, com defaults.
  → [map.mh](syntax/higher_order_array/map.mh) ·
  [omitted_argument_uses_the_default.mh](syntax/param_defaults/omitted_argument_uses_the_default.mh)
- **`list_tools()`** (mcp) — `tools/list`. → [stdio_mock_server_proves_list_tools_wiring.mh](features/mcp/stdio_mock_server_proves_list_tools_wiring.mh)
- **`log`** — `log.info(...)` (namespace nativo) e a propriedade `log:` de agent (caminho
  interpolado por run). → [session_scoped_memory_pipeline_example.mh](features/pipelines/session_scoped_memory_pipeline_example.mh) ·
  [agent_log_path_interpolates_per_run.mh](features/agents/agent_log_path_interpolates_per_run.mh)
- **`log_mode`** (agent) — `"truncate"` para reiniciar o log a cada tentativa.
  → [agent_runs_a_cli_without_a_wrapper.mh](features/agents/agent_runs_a_cli_without_a_wrapper.mh)
- **Lógicos** (`&&`, `||`). → [syntax/logical_operations/](syntax/logical_operations/README.md)
- **`loop pipeline` / `loop workflow`** — repete o corpo até `stop_when` ou `max_iterations`.
  → [loop_poll_pipeline_example.mh](features/pipelines/loop_poll_pipeline_example.mh) ·
  [linear_pipeline_and_branching_workflow.mh](syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh)

## M

- **`.map()`** — transforma cada elemento. → [map.mh](syntax/higher_order_array/map.mh)
- **`match`** — ramificação multivia por padrão, exaustiva sobre `enum`/`bool`.
  → [syntax/enum_and_match/](syntax/enum_and_match/README.md)
- **`.matches()`** — regex RE2 sobre a string inteira. → [matches.mh](syntax/string_methods/matches.mh)
- **`max <N>`** — teto de iterações no cabeçalho de `loop workflow`.
  → [linear_pipeline_and_branching_workflow.mh](syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh)
- **`max_concurrency`** (`spawn: { ... }`) — limite de chamadas em segundo plano.
  → [concurrent_agents_pipeline_example.mh](features/pipelines/concurrent_agents_pipeline_example.mh)
- **`max_iterations`** (`repeat: { ... }`). → [loop_poll_pipeline_example.mh](features/pipelines/loop_poll_pipeline_example.mh)
- **`mcp`** (extension) — cliente MCP stdio/http: `.call()`, `.list_tools()`, `.discover()`.
  → [features/mcp/](features/mcp/README.md)
- **`memory`** — stores `json` (chave/valor) e `append_log`; `path:` interpolado por sessão.
  → [features/memory/](features/memory/README.md) ·
  [session_scoped_memory_pipeline_example.mh](features/pipelines/session_scoped_memory_pipeline_example.mh)
- **`merge`** — junção explícita de um grupo `parallel`.
  → [parallel_merge_workflow_example.mh](features/pipelines/parallel_merge_workflow_example.mh) ·
  [parallel_group_vars_workflow_example.mh](features/pipelines/parallel_group_vars_workflow_example.mh)

## N

- **`nameof()`** — nome de uma declaração, checado pelo lint e navegável no editor.
  → [router_select_uses_nameof_for_ide_navigation.mh](features/router/router_select_uses_nameof_for_ide_navigation.mh) ·
  [skills_compose_system_prompt_from_selected_skills.mh](features/skills/skills_compose_system_prompt_from_selected_skills.mh)
- **Argumento nomeado abreviado** `f(name:)`. → [named_argument_shorthand.mh](syntax/shorthand/named_argument_shorthand.mh)
- **`??`** (null-coalescing) — valor padrão quando `null`.
  → [coalesce_supplies_a_default.mh](syntax/optional_access/coalesce_supplies_a_default.mh) ·
  [coalesce_chains_left_to_right.mh](syntax/optional_access/coalesce_chains_left_to_right.mh)

## O

- **Objetos** — literais, `.key`, `obj[key]`, escrita dinâmica.
  → [syntax/object_property_access/](syntax/object_property_access/README.md)
- **Objeto abreviado** `{artifact, path}`. → [object_field_shorthand.mh](syntax/shorthand/object_field_shorthand.mh)
- **`ollama/*`** (engine de agent) — servidor Ollama local. → [ollama_cli_calls_the_real_api.mh](features/agents/ollama_cli_calls_the_real_api.mh)
- **`os.*`** — `platform`, `arch`, `hostname`, `cwd`, `pid`, `user`, `home_dir`, `executable`.
  → [os_platform_and_arch_are_recognized_values.mh](features/os/os_platform_and_arch_are_recognized_values.mh) ·
  [os_hostname_cwd_and_pid_are_reported.mh](features/os/os_hostname_cwd_and_pid_are_reported.mh) ·
  [os_user_and_home_dir_are_non_empty.mh](features/os/os_user_and_home_dir_are_non_empty.mh) ·
  [os_executable_returns_the_running_binary_path.mh](features/os/os_executable_returns_the_running_binary_path.mh)
- **`output:`** — projeção explícita do resultado de um workflow.
  → [run_workflow_output_projection.mh](features/test_run_workflow/run_workflow_output_projection.mh) ·
  [run_workflow_output_contract_fails.mh](features/test_run_workflow/run_workflow_output_contract_fails.mh)
- **`?.` / `?.[expr]`** (acesso opcional). → [syntax/optional_access/](syntax/optional_access/README.md)

## P

- **`parallel`** — grupo de steps concorrentes com barreira; `var`s do grupo; `timeout`.
  → [parallel_steps_pipeline_example.mh](features/pipelines/parallel_steps_pipeline_example.mh) ·
  [parallel_group_vars_workflow_example.mh](features/pipelines/parallel_group_vars_workflow_example.mh)
- **Parâmetros com default** — em métodos de `tool`, lambdas e `prompt`s.
  → [syntax/param_defaults/](syntax/param_defaults/README.md) ·
  [prompt_default_parameter.mh](features/prompts/prompt_default_parameter.mh)
- **`pause()`** — suspende o run (HITL) até um `--resume`.
  → [run_workflow_reports_pause.mh](features/test_run_workflow/run_workflow_reports_pause.mh) ·
  [goto_match_artifact_review.mh](syntax/pipeline_vs_workflow/goto_match_artifact_review.mh)
- **`pipeline`** — unidade de execução linear.
  → [features/pipelines/](features/pipelines/README.md) ·
  [syntax/pipeline_vs_workflow/](syntax/pipeline_vs_workflow/README.md)
- **`prompt`** — template declarado, chamado como função, aninhável.
  → [features/prompts/](features/prompts/README.md)

## Q

- **`query`** (opção de `http.*`) — parâmetros de query string.
  → [http_verbs_share_one_option_surface.mh](features/http/http_verbs_share_one_option_surface.mh)

## R

- **`rate_limit`** (agent) — `on_exceeded: "queue"`.
  → [agent_retry_cache_rate_limit_reject_unimplemented_values.mh](features/agents/agent_retry_cache_rate_limit_reject_unimplemented_values.mh)
- **`.reduce()`** — dobra a partir de um valor inicial. → [reduce.mh](syntax/higher_order_array/reduce.mh)
- **`ref { ... }` / `ref (expr)`** — objeto compartilhado por referência, identidade preservada no resume.
  → [ref_objects_are_shared.mh](syntax/value_vs_ref/ref_objects_are_shared.mh)
- **`.remove()` / `.remove_content()` / `.replace()`** — edição de strings.
  → [remove.mh](syntax/string_methods/remove.mh) · [remove_content.mh](syntax/string_methods/remove_content.mh) ·
  [replace.mh](syntax/string_methods/replace.mh)
- **`repeat`** — política de repetição de `loop pipeline` (`stop_when`, `max_iterations`).
  → [loop_poll_pipeline_example.mh](features/pipelines/loop_poll_pipeline_example.mh)
- **`--resume`** — continua de um checkpoint/pausa.
  → [syntax/pipeline_vs_workflow/](syntax/pipeline_vs_workflow/README.md) ·
  [features/graph_and_events/](features/graph_and_events/README.md)
- **`retry` / `retry_on`** (agent) — novas tentativas com backoff exponencial.
  → [agent_timeout_stops_a_hung_process.mh](features/agents/agent_timeout_stops_a_hung_process.mh) ·
  [gemini_api_calls_the_real_api.mh](features/agents/gemini_api_calls_the_real_api.mh)
- **`route`** — tabela de `goto match` reutilizável dentro de um workflow; com `entry`, é o
  ponto de partida do run ([`entry route`](#e)).
  → [goto_match_artifact_review.mh](syntax/pipeline_vs_workflow/goto_match_artifact_review.mh) ·
  [order_triage.mh](features/graph_and_events/order_triage.mh)
- **`router`** — escolhe um agent por `select` determinístico e/ou `decider`.
  → [features/router/](features/router/README.md) · [fixtures/router.mh](features/router/fixtures/router.mh)
- **`.run()`** — de um agent ([features/agents/](features/agents/README.md)); de um
  pipeline/workflow dentro de testes ([features/test_run_workflow/](features/test_run_workflow/README.md)).

## S

- **`schema`** — `schema Name from "file.json"` → `{content, path}`; `schema:` em `.run()`.
  → [features/schemas/](features/schemas/README.md) ·
  [gemini_structured_api_splices_schema_as_a_json_object.mh](features/agents/gemini_structured_api_splices_schema_as_a_json_object.mh)
- **`select`** (router) — hook determinístico; chamável sem delegar.
  → [router_select_hook_resolves_deterministically.mh](features/router/router_select_hook_resolves_deterministically.mh) ·
  [router_select_is_callable_without_delegating.mh](features/router/router_select_is_callable_without_delegating.mh)
- **`self.`** — variáveis do pipeline/grupo e métodos internos de `tool`.
  → [assign_destructuring.mh](syntax/destructuring/assign_destructuring.mh) ·
  [internal_method.mh](syntax/tool_scope/internal_method.mh)
- **`.send()`** (a2a). → [a2a_send_calls_the_real_api.mh](features/a2a/a2a_send_calls_the_real_api.mh)
- **Assinatura tipada** `workflow X(req: In): Out` (campos opcionais `base?: T`).
  → [run_workflow_typed_signature.mh](features/test_run_workflow/run_workflow_typed_signature.mh)
- **`.size()`** — tamanho de array/string. → [array_statements.mh](syntax/array_statements/array_statements.mh)
- **`skill` / `skills:` / `system_prompt`** — instruções em Markdown compostas no prompt de sistema.
  → [features/skills/](features/skills/README.md)
- **Slicing** `[a..b]`, `^` (a partir do fim). → [slice_array.mh](syntax/array_statements/slice_array.mh)
- **`.sort_by()`** — ordena por chave numérica/textual. → [sort_by_number.mh](syntax/higher_order_array/sort_by_number.mh)
- **`spawn` / `wait`** — chamadas de agent em segundo plano e junção (`wait any`, `wait N of`, fan-out `for`).
  → [concurrent_agents_pipeline_example.mh](features/pipelines/concurrent_agents_pipeline_example.mh)
- **`.split()` / `.starts_with()` / `.ends_with()` / `.substring()`**.
  → [syntax/string_methods/](syntax/string_methods/README.md)
- **Spread `...xs`** — em literais de array e `args`.
  → [agent_takes_inputs_and_spreads_args.mh](features/agents/agent_takes_inputs_and_spreads_args.mh) ·
  [parallel_merge_workflow_example.mh](features/pipelines/parallel_merge_workflow_example.mh)
- **`stdin`** (agent) — entrega o prompt pela entrada padrão, fora do argv.
  → [agent_stdin_keeps_large_content_out_of_argv.mh](features/agents/agent_stdin_keeps_large_content_out_of_argv.mh)
- **`step`** — unidade de trabalho de um pipeline. → [agent_pipeline_example.mh](features/pipelines/agent_pipeline_example.mh)
- **`stop_when`** — condição de parada de `loop pipeline`. → [loop_poll_pipeline_example.mh](features/pipelines/loop_poll_pipeline_example.mh)
- **Strings** — métodos embutidos. → [syntax/string_methods/](syntax/string_methods/README.md)

## T

- **`test` / `describe`** — testes autoverificáveis rodados por `mhl test`; todo arquivo de
  [syntax/](syntax/README.md) é um exemplo. Exercitar um workflow inteiro: [features/test_run_workflow/](features/test_run_workflow/README.md)
- **`time.*`** — `now`, `parse`, `format`, `add`, `diff`, `compare`, `sleep` (datas como strings RFC3339).
  → [features/time/](features/time/README.md) · [time_sleep_pauses_execution.mh](features/time/time_sleep_pauses_execution.mh)
- **`timeout`** — por tentativa de agent, e no cabeçalho de `step`/`parallel` (`step Build timeout 10m`).
  → [agent_timeout_stops_a_hung_process.mh](features/agents/agent_timeout_stops_a_hung_process.mh) ·
  [linear_pipeline_and_branching_workflow.mh](syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh) ·
  [order_triage.mh](features/graph_and_events/order_triage.mh)
- **`.to_upper()` / `.to_lower()` / `.trim()`**.
  → [to_upper_and_to_lower.mh](syntax/string_methods/to_upper_and_to_lower.mh) · [trim.mh](syntax/string_methods/trim.mh)
- **`tool`** — métodos tipados com `const`/`var` de escopo de chamada.
  → [syntax/tool_scope/](syntax/tool_scope/README.md)
- **`try` / `catch` / `finally`** — captura de erros.
  → [object_property_access_with_exception.mh](syntax/object_property_access/object_property_access_with_exception.mh) ·
  [agent_timeout_stops_a_hung_process.mh](features/agents/agent_timeout_stops_a_hung_process.mh)
- **`type`** (alias) — `type Name = <TypeExpr>`. → [syntax/type_alias/](syntax/type_alias/README.md)
- **`type_of()`**. → [syntax/type_introspection/](syntax/type_introspection/README.md)

## U

- **`.unique()`** — remove duplicatas profundas. → [unique_keeps_first_occurrence.mh](syntax/array_collection_ops/unique_keeps_first_occurrence.mh)
- **`uuid.v4` / `uuid.v7`**. → [features/uuid/](features/uuid/README.md)

## V

- **Valor vs. referência** — objetos e arrays são copiados; `ref` é compartilhado.
  → [syntax/value_vs_ref/](syntax/value_vs_ref/README.md)
- **`.values()`** — valores de um objeto. → [object_get_keys_and_values.mh](syntax/object_property_access/object_get_keys_and_values.mh)
- **`var`** — variável mutável (local, de pipeline, de `tool`, de grupo `parallel`).
  → [var_initializes_in_order_and_resets_per_call.mh](syntax/tool_scope/var_initializes_in_order_and_resets_per_call.mh) ·
  [parallel_group_vars_workflow_example.mh](features/pipelines/parallel_group_vars_workflow_example.mh)

## W

- **`wait`** — ver [`spawn`](#s).
- **`while`** — laço condicional. → [const_holds_a_single_value.mh](syntax/const_declaration/const_holds_a_single_value.mh)
- **`with`** — `obj with { campo: valor }` copia e sobrescreve; `workflow A = B with { ... }` cria um alias.
  → [with_copies_and_overrides_fields.mh](syntax/value_vs_ref/with_copies_and_overrides_fields.mh) ·
  [run_workflow_alias.mh](features/test_run_workflow/run_workflow_alias.mh)
- **`workflow`** — como `pipeline`, mas aceita `goto`.
  → [syntax/pipeline_vs_workflow/](syntax/pipeline_vs_workflow/README.md) ·
  [features/test_run_workflow/](features/test_run_workflow/README.md)

## X

- **`x-mcp-header`** — anotação MCP sobre transporte `http`; sem exemplo `.mh` (explicação em
  [features/mcp/](features/mcp/README.md)).

---

## Recursos ainda sem exemplo aqui

- **Hooks de ciclo de vida de pipeline/workflow** — `session_start`, `session_end`,
  `step_start`, `step_end`, `stop_failure` (tipos `SessionContext`, `StepContext`,
  `FailureContext`). Ver [Docs-Reference.dc.html](../docs/site/Docs-Reference.dc.html).
