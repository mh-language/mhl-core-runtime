# Melhorias sugeridas para a linguagem MHL

> Feedback para quem mantém o compilador/runtime do MHL — não é trabalho do projeto Senpai. Cada item nasceu de um comportamento real observado (spike, `mhl lint`/`mhl run` real, ou uma lacuna de documentação) ao construir o Senpai em MHL `1.4.0-beta.5`, não de uma preferência abstrata. Referência cruzada: [FASE0-ACHADOS.md](FASE0-ACHADOS.md) tem o relato completo de cada spike citado aqui. Atualizar esta lista sempre que uma nova fase esbarrar em outra lacuna.

## 1. `agent.command`/`args` deveriam aceitar uma expressão, não só string literal — ✅ Resolvido

Hoje `command: env("VAR")` falha com `agent "X" has no command` — só uma string literal é aceita (confirmado em spike, [FASE0-ACHADOS.md §1](FASE0-ACHADOS.md#1-commandargs-do-agent-não-aceitam-env--só-string-literal)). Isso força qualquer produto que precise trocar de backend de LLM por ambiente (dev vs. produção) a manter arquivos `.mh` de agente **duplicados**, um por ambiente, escolhidos por qual é importado — em vez de uma única declaração parametrizada por `env(...)`/`context.*`. Permitir `command:`/`args:` avaliarem uma expressão (ainda que só no momento da declaração, não por chamada) removeria essa duplicação estrutural.

> **Status:** `command`/cada elemento de `args` agora aceitam qualquer expressão (`env(...)`, concatenação, variável), não só string literal — mesmo tratamento que `extension { endpoint: env(...) }` já tinha. Um literal de string continua sendo usado ao pé da letra (sem interpolar `${...}`), preservando os placeholders `"${prompt}"`/`"${schema}"`. Uma referência de credencial em `env(...)` falha fechado (mesma garantia de `extension`) e é registrada para redaction. `mhl lint` para de reportar falso "has no command"/"args must be an array" para uma expressão não-literal. Implementado em `internal/engine/interpreter/agent.go` (`resolveAgentCommand`/`resolveCommandLikeExpr`) e `internal/lang/ast/agentconfig.go`; testes em `agent_engine_test.go`, `lint_test.go`.

## 2. `mhl serve mcp` (stdio) deveria expor os mesmos `mhl_run_*` que o modo `--http` — 🚧 Blocking

Confirmado por spike: a variante stdio (`mhl serve mcp <dir>`) só publica os workflows como ferramentas síncronas — `tools/list` nunca inclui `mhl_run_start`/`status`/`resume`/`cancel`/`list`/`logs`, e chamá-los retorna `unknown tool`. Isso obriga qualquer aplicação desktop que só precisa de um processo filho local (sem nenhuma intenção de expor rede) a abrir uma porta HTTP em loopback só para ganhar acesso a progresso assíncrono e human-in-the-loop — que são conceitos de protocolo (MCP), não de transporte. Achado completo: [FASE0-ACHADOS.md §2](FASE0-ACHADOS.md#2-achado-crítico--mhl_run_startstatusresumecancellistlogs-só-existem-sob---http-nunca-em-stdio-puro).

**Sugestão:** publicar os `mhl_run_*` também sobre stdio (mesmo protocolo JSON-RPC, sem sessão HTTP) — o "dono" de um run em stdio seria naturalmente o único processo cliente do outro lado do pipe, então nem precisaria do conceito de `Mcp-Session-Id`.

## 3. `memory { path: "...${var}..." }` deveria ter um modo seguro contra path traversal — ✅ Resolvido

Confirmado por spike ([FASE0-ACHADOS.md §7](FASE0-ACHADOS.md#7-achado-crítico-de-segurança--memory--path--interpola-sem-sanitizar-path-traversal-funciona-de-verdade)): um `project_id` como `"../../../../tmp/x"` interpolado em `path: "projects/${project_id}/f.json"` escreve de verdade fora do diretório pretendido — a interpolação de `${...}` em `path:` não faz nenhuma validação. Isso é um footgun sério para qualquer app multi-tenant/multi-projeto construída em MHL (isolamento por diretório é um padrão comum). Hoje a única defesa é 100% do código do usuário (nosso `tool Paths`), nunca do runtime.

**Sugestão:** ao menos uma opção declarativa — `memory { path: "...", path_guard: "no_traversal" }` (ou equivalente) — que rejeite (`fail`) automaticamente um valor interpolado contendo `..` ou um caminho absoluto antes de tocar o disco. Não precisa ser o padrão (quebraria casos legítimos), mas hoje não existe nem como opt-in.

> **Status:** implementado exatamente como sugerido — `memory { path_guard: "no_traversal" }` (opt-in, não é o padrão). A checagem é por *span* interpolado (o valor de cada `${...}`), não na string final concatenada — assim um `path:` cujo template já começa de um diretório absoluto continua válido; só o segmento interpolado (ex. `project_id`) é barrado se contiver `..` ou tiver cara de caminho absoluto. Qualquer outro valor de `path_guard` é erro em tempo de lint. Implementado em `internal/engine/interpreter/memory_ops.go` (`memoryPath`/`guardPathSpan`) e `internal/engine/interpreter/interpolate.go` (`interpolateChecked`); testes em `memory_test.go` (cenário de traversal real bloqueado) e `lint_test.go`.

## 4. `html.escape`/`html.attr_escape` nativos — ✅ Resolvido

O bloco de operações nativas de `html` já tem `parse`, `get_element(s)`, `get_element_by_id`, `get_attribute`, `get_text`, `to_html` — mas nenhum `escape`. Qualquer geração de HTML a partir de texto de LLM (nosso caso de uso central em `Discovery`/`Delivery`) precisa reimplementar escaping à mão como uma cadeia de `.replace("&","&amp;").replace("<","&lt;")...`, na ordem certa, sem checagem do compilador de que a ordem/cobertura está correta. Isso é exatamente o tipo de operação que deveria ser nativa e testada pelo próprio runtime, do mesmo jeito que `json.stringify` já cobre escaping de string JSON.

> **Status:** `html.escape(text)` e `html.attr_escape(text)` implementados — os dois delegam para o escaping de `golang.org/x/net/html` (`<`, `>`, `&`, `'`, `"`), que já cobre tanto conteúdo de texto quanto valor de atributo entre aspas simples ou duplas; existem como dois nomes por clareza no call-site, não porque a regra de escaping seja diferente entre eles. Implementado em `internal/features/nativeops/html.go` (`EscapeHTML`) + `internal/engine/interpreter/tool.go`; catálogo do LSP atualizado; testes em `html_test.go` (Go) e `sample/features/html/html_escape_and_attr_escape.mh` (round-trip real: escapar, montar HTML, fazer parse de volta e comparar).

## 5. `mhl lint` tem falso-positivo em `<array-var>.append(x)` — ✅ Resolvido

`items.append(x)` numa `var` array simples (nunca declarada como `memory`) é sinalizado por `mhl lint` como `memory "items" not found` — o linter parece resolver `<nome>.append(...)` tentando primeiro achar uma declaração `memory` com esse nome (já que `memory { type: "append_log" | "jsonl" }` também expõe um método `append`), e erra quando não existe, mesmo o `mhl run`/`mhl test` executando o array `.append()` corretamente. Já existe um caso irmão documentado nos próprios docs oficiais (`json memory has no method "remove"` como falso positivo conhecido) — sugerimos tratar este da mesma forma: como um item de correção do lint, não do usuário. Achado completo com repro mínimo: [FASE0-ACHADOS.md §8](FASE0-ACHADOS.md#8-achados-de-sintaxe-descobertos-escrevendo-a-fase-1-tool-paths--workflow-workitem).

> **Status:** corrigido — antes de cair no fallback "memory not found" para um método que colide com nome de método de `memory` (`set`/`get`/`append`/`remove`), o lint agora checa se o alvo já é uma variável local conhecida (`var`/`const`/parâmetro/`spawn`/`for-in`) e, se for, não emite achado nenhum (é um método de valor comum, resolvido em runtime). Implementado em `internal/lang/lint/checks.go` (`checkExprCallShape`); teste `TestLocalArrayAppendNotFlaggedAsMemory` em `lint_test.go`.

## 6. Chamada de método-irmão dentro de um `tool` deveria funcionar sem qualificação — 🚧 Blocking

Dentro de `tool Paths { root(id) -> "projects/" + ensure_valid(id) }`, chamar `ensure_valid(id)` sem prefixo falha com `undefined variable "ensure_valid"` — é preciso escrever `Paths.ensure_valid(id)`, mesmo estando no mesmo bloco declarativo. Isso não está documentado nas páginas de referência lidas e é uma pequena superfície a mais de fricção/erro (o padrão em praticamente toda outra linguagem com namespaces de método — `this.foo()` ou simplesmente `foo()` dentro da própria classe/módulo — é não precisar do nome completo). Se for intencional (por exemplo, para manter o corpo do método sem "escopo implícito" nenhum), vale pelo menos documentar explicitamente na página de `tool`.

## 7. Escrita de campo de objeto via `.` (não só `[...]`) — 🚧 Blocking

Leitura funciona com `obj.campo`, mas escrita (`obj.campo = valor`) falha com `assignment target must be a plain variable or an array index, not a nested field` — é preciso `obj["campo"] = valor`. A assimetria entre leitura (ponto) e escrita (colchete) para o mesmo tipo de acesso é uma pegadinha discreta; poucos usuários vão adivinhar que `obj.campo = x` é rejeitado antes de tentar.

## 8. `input` de `pipeline`/`workflow` não aceita valor default — ✅ Resolvido

Só `parameter` (de `tool`/lambda) aceita `= expressão`; `input` é sempre obrigatório. Como cada `pipeline`/`workflow` vira uma ferramenta MCP com "every input required" (Docs-Servers §01), isso significa que um workflow com múltiplas ações (nosso `WorkItem`, por exemplo, com `create`/`list`/`get`/`archive`/`usage`) obriga o chamador a sempre passar **todos** os inputs em toda chamada, mesmo os irrelevantes para a ação escolhida (`item_type: ""` numa chamada `action: "list"`, por exemplo) — o esquema JSON exposto via `tools/list` também fica menos preciso (marca `item_type` como obrigatório mesmo quando é ignorado por 4 das 5 ações). Um `input x: Type = default` opcional deixaria tanto o `.mh` quanto o `inputSchema` derivado mais honestos sobre o que cada ação realmente usa.

> **Status:** `input name: Type = expr` agora é aceito, com a mesma sintaxe de `parameter`. Um input com default vira opcional: some do `required` do `inputSchema` derivado e, quando o default é um literal, aparece também como `"default"` no schema JSON. `ValidateInputs` não marca mais o input como `missing` quando ausente. A expressão do default é avaliada uma vez por execução (mesmo timing de um `var` de pipeline) e um valor explicitamente passado pelo chamador sempre vence sobre o default. Implementado em `internal/lang/ast/pipeline.go`, `internal/engine/runtime/pipeline.go` + `schema.go`, `internal/engine/interpreter/exec.go` (`EvalPipelineVars`); testes em `schema_test.go`, `execsvc_test.go`, `parser_test.go`.

## 9. Enums em `input` de workflow servido por MCP — ✅ Resolvido

Não testamos still, mas a tabela de "Declarable types" não deixa claro se um `input action: ActionType` (com `enum ActionType { Create, List, Get, Archive, Usage }`) é aceito e, se for, como isso se projeta no `inputSchema` JSON exposto via `tools/list` (JSON Schema tem `enum` nativo — seria o mapeamento natural). Se funcionar, seria a forma correta de fechar valores como `action`/`type` em vez de string livre + validação manual dentro do step. Vale um spike futuro dedicado; hoje é registrado aqui como lacuna de documentação, não como bug confirmado.

> **Status:** confirmado por investigação de código (não só spike): `input action: ActionType` já funcionava e já era aceito antes desta mudança, mas projetava no schema só como `{"type": "string"}` genérico — a lacuna real não era "funciona ou não", era a falta do `"enum": [...]` no JSON Schema. Corrigido: `runtime.FindPipeline` agora resolve os variants declarados do `enum` e `InputSchema()` os inclui como `"enum": ["Create", "List", ...]`, na ordem de declaração — o mapeamento nativo do JSON Schema que o item já esperava. Implementado em `internal/engine/runtime/pipeline.go` (`enumVariants`, `PipelineInputSpec.EnumVariants`) e `schema.go`; testes em `schema_test.go` (unit + end-to-end via `FindPipeline` com `enum` real).

## 10. `time.format`/`time.parse` — escapar literais nos tokens amigáveis (`yyyy`/`MM`/…) — 🚧 Blocking

O layout "amigável" (`yyyy-MM-dd`) não tem mecanismo de escape para caracteres literais no meio do padrão — `"yyyy-MM-dd'T'HH:mm:ss'Z'"` (sintaxe de aspas simples do Java/ICU) produz literalmente as aspas na saída em vez de tratar `T`/`Z` como literais. Hoje o único jeito de conseguir um timestamp ISO 8601/RFC 3339 correto é abandonar os tokens amigáveis e usar o layout Go puro (`"2006-01-02T15:04:05Z07:00"`) — o que exige conhecer a convenção de referência do Go (`Mon Jan 2 15:04:05 MST 2006`), justamente o conhecimento que os tokens amigáveis deveriam evitar exigir. Um mecanismo de escape (aspas simples, como ICU, ou colchetes) fecharia essa lacuna sem quebrar compatibilidade.

## 11. Contrato de saída estruturada (`schema:`) deveria ser uniforme entre adapters `cli/*` — 🚧 Blocking

Hoje, na prática (não documentado, descoberto testando os três CLIs reais):
- `claude`: `--json-schema <schema-inline>` — schema como string JSON direta no argv.
- `codex`: `codex exec --output-schema <arquivo>` — schema como **caminho de arquivo**, não inline.
- `devin`: **nenhuma flag de saída estruturada** em `--help`/`-p`.

Um usuário que declara `agent.run(prompt:, schema:)` esperando o mesmo comportamento documentado ("passed to backends that support structured output") precisa descobrir por tentativa que `codex` exige gravar o schema em disco antes da chamada, e que `devin` simplesmente não tem como cumprir o contrato via CLI nenhuma. Se o adapter `cli/*` do MHL abstraísse essas diferenças (escrevendo o arquivo temporário sozinho quando o CLI exigir um caminho, e falhando de forma clara e antecipada — não silenciosa — quando o CLI não suportar nada), `schema:` seria de fato portável entre backends, como o texto da documentação sugere que já é.

## 12. `mhl run --format json` pode emitir JSON tecnicamente inválido — 🚧 Blocking

O campo `log` do envelope de saída às vezes contém quebras de linha reais (não escapadas como `\n`) dentro do valor da string, o que faz um parser JSON estrito (`json.loads` do Python, por exemplo) rejeitar o documento inteiro com "Invalid control character". Um consumidor programático de `--format json` (o próprio caso de uso do flag) não deveria precisar de um parser tolerante a JSON malformado.

## 13. Testar um `pipeline`/`workflow` inteiro (steps + `goto` + `pause`) de dentro de `test`/`describe` — 🚧 Blocking

O bloco `test`/`describe` hoje só exercita `tool`s e expressões — não há um jeito documentado de rodar um `workflow` inteiro (com seus `step`s, `goto`s e `pause()`s) a partir de um `test`, validando `reached`/`state` como se faz manualmente via `mhl run --format json`. Isso deixa um buraco de cobertura automatizada: hoje a única forma de testar o *fluxo* de um `workflow` (não só a lógica pura dentro de um `tool`) é rodar `mhl run` de verdade e inspecionar a saída manualmente/via script externo, fora do framework de teste nativo do MHL.

## 14. Suporte a classes de caracteres/regex simples em `string` — ✅ Resolvido

Validar que um identificador só contém `[a-zA-Z0-9_-]` hoje exige `split("")` + iterar caractere por caractere comparando contra uma string de caracteres permitidos (`allowed.contains(c)`) — funciona, mas é código de baixo nível para uma tarefa comum (slugs, ids, validação de formulário). Um método `matches(pattern: string): bool` (regex simples) ou ao menos predicados de classe (`is_alnum()`, `is_ascii_letter()` por caractere) reduziria esse boilerplate em qualquer `tool` de validação.

> **Status:** `string.matches(pattern: string): bool` implementado — regex RE2 (sintaxe do pacote `regexp` do Go), comparando a string **inteira** contra o padrão (convenção Java/Kotlin, não "contém um match" como JS/C#), e um padrão inválido levanta erro catchable. Implementado em `internal/engine/interpreter/eval.go` (`callValueMethod`); catálogo do LSP atualizado em `internal/lsp/signatures.go`/`symbols.go`; exemplos em `sample/syntax/string_methods/matches.mh` e `matches_invalid_pattern_errors.mh`.
