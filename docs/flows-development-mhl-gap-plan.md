# Plano de implementação: fechar os gaps de `flows-development-mhl-coverage.md`

Plano para resolver todos os itens marcados ⚠️ Workaround e ❌ Gap na tabela de
[flows-development-mhl-coverage.md](flows-development-mhl-coverage.md). Organizado em
fases sequenciais — cada fase é independente e entregável sozinha, mas a ordem segue
a de "Onde começar" (maior desbloqueio por menor esforço primeiro).

Todo item novo segue o padrão já estabelecido no runtime: função Go pura em
`internal/features/nativeops/*.go` (sem conceitos de MHL, só `context`/tipos nativos),
dispatch em `internal/engine/interpreter/tool.go` (`nativeOpCall`) ou, para métodos de
valor (string/array), em `callValueMethod` (`internal/engine/interpreter/eval.go`).

## Fase 0 — Fundação (desbloqueia tudo abaixo) — ✅ Concluída

### 0.1 `json.stringify(value)` — fecha o gap de serialização — ✅ Implementado

- **Arquivo:** `internal/features/nativeops/json.go` — `func Stringify(v any) (string, error)` usando `json.Marshal`, espelhando o doc comment de `Parse`.
- **Dispatch:** `internal/engine/interpreter/tool.go`, `nativeOpCall`, case `"json.stringify"` (mesmo padrão do `"json.parse"`, um argumento posicional qualquer).
- **Teste:** `internal/features/nativeops/json_test.go` (round-trip parse→stringify→parse, escalares/nil, erro em valor não serializável) + `internal/cli/tool_test.go` (`TestToolJSONStringifyThenParseRoundTrips`, end-to-end via `.mh`) + cenário real em `test/e2e/features/test_memory.mh` (`memory_session_mem_stringify_roundtrips_through_a_file`: pega valor de `memory`, serializa, grava com `fs.write`, relê com `fs.read`+`json.parse`).

### 0.2 `cmd.exec` aceitando array de args — fecha o bloqueador de quoting — ✅ Implementado

- **Arquivo:** `internal/features/nativeops/cmd.go` — `Exec(ctx, command string, timeout)` (existente, `strings.Fields`) mantido intacto; nova `ExecArgs(ctx, argv []string, timeout) (map[string]any, error)` pula o split e chama `tools.Exec(ctx, argv[0], argv[1:]...)` direto.
- **Dispatch:** `tool.go`, case `"cmd.exec"` — novo helper `callArgs.stringSliceAt(i)` detecta se `args.positional[0]` é `[]any`; se for (e todos os elementos forem string), roteia para `ExecArgs`; senão cai no caminho antigo (`stringAt` + `Exec`). Sem quebra de compatibilidade.
- **Teste:** `cmd_test.go` (`TestExecArgsPreservesSpacesInASingleArgument`, non-zero exit, argv vazio, timeout) + `tool_test.go` (`TestToolCmdExecArgvArrayPreservesSpacesInAnArgument`, erro em elemento não-string) + `test_memory.mh` (`memory_session_mem_value_survives_cmd_exec_argv`: valor de `memory` com espaço sobrevive intacto por `cmd.exec(["echo","-n",message])`).
- **Confirmado:** `cmd.exec(["git","commit","-m","mensagem com espaço"])` agora produz um único argv — o bloqueador original está resolvido.

### Achado durante a Fase 0: bug pré-existente em `memory` (fora de escopo)

Ao escrever o teste e2e em `test_memory.mh`, descobri que `AuditLogMem.read()` — já
presente no arquivo antes desta fase — **nunca foi implementado**: `isMemoryMethod()`
(`memory_ops.go:19-21`) não inclui `"read"` na whitelist, o case `"append_log"` em
`executeMemoryOp` (`memory_ops.go:145-163`) só trata `.append()`, e não existe
`memory.Read()` do lado Go (`appendlog.go` só tem `Append`). Isso quebra a execução do
arquivo inteiro via `mhl test test_memory.mh` (erro fatal, não uma assertion falhando) —
os testes novos desta fase passam isoladamente, mas não quando o arquivo roda por
inteiro. Não é um item desta tabela/plano (é bug de `memory`, não de `Flows.Development`
coverage) — decisão pendente do usuário se entra num fix separado.

## Fase 1 — Git nativo (fecha o gap ❌ "Git: add/commit/status/rev-parse/log") — ✅ Concluída

Mesmo padrão de `git.Diff` (`internal/features/nativeops/git.go:17`), todas via argv
(nunca string+split), então já nascem imunes ao problema de quoting. Nenhuma recebe um
`targetDir` explícito — como `Diff`, operam sobre o cwd do processo (`tools.Exec` não
seta `cmd.Dir`); um pipeline .mh que precisar rodar num diretório-alvo específico
resolve isso com caminhos absolutos, não com um parâmetro de diretório por chamada.

- `Add(ctx, paths []string) (map[string]any, error)` → `git add -A -- <paths...>` — implementado
- `Commit(ctx, message string, paths []string) (map[string]any, error)` → `git commit -m <message> [-- <paths...>]` — implementado
- `Status(ctx, paths []string) (map[string]any, error)` → `git status --short [-- <paths...>]` — implementado (retorna estruturado, não string crua — ver nota abaixo)
- `RevParse(ctx, ref string) (string, error)` → `git rev-parse --short <ref>` (mirror de `Diff`, retorna string crua) — implementado
- `Log(ctx, n int) (string, error)` → `git log -n <n> --oneline` (mirror de `Diff`) — implementado

- **Dispatch:** `tool.go`, casos `"git.add"`, `"git.commit"`, `"git.status"`, `"git.rev_parse"`, `"git.log"` — `paths` é opcional (nomeado `paths:` ou posicional) em add/commit/status; `add` exige pelo menos um path (erro se vazio/ausente).
- **Retorno:** `add`/`commit`/`status` retornam `{stdout, stderr, exit_code}` (igual `cmd.exec`, porque o chamador precisa checar exit code — ex.: nada staged faz `git commit` sair non-zero sem ser um erro Go); `rev_parse`/`log` retornam string crua (igual `Diff`) — **ajustado em relação ao sketch original**, que listava `Status` retornando string; ficou estruturado porque `git status --short --quiet`-style checks (ex.: "árvore limpa?") precisam do exit code, não só do texto.
- **Novos helpers em `callArgs`** (`tool.go`): `stringSliceNamedOrAt`, `intNamedOrAt`, e `toStringSlice` compartilhado com `stringSliceAt` (refatorado).
- **Teste:** `git_test.go` num repo git temporário (`t.TempDir()` + `git init`) — `TestHandoffCycleAddCommitStatusRevParse` replica o ciclo real do `Handoff` do C# (add → commit com mensagem com espaço → status limpo → rev-parse → log), mais casos de erro (paths vazio, mensagem vazia, commit sem nada staged, ref desconhecida, n não-positivo). `internal/cli/tool_test.go`: `TestGitHandoffCycle` faz o mesmo ciclo end-to-end via `.mh` através do dispatch da linguagem, também isolado num `t.TempDir()`.
- **Sem cenário `.mh` em `test/e2e/`:** cheguei a escrever `test/e2e/features/test_git.mh` (leituras `git.status`/`git.rev_parse`/`git.log` + erros de validação que retornam antes de tocar o git), mas removi — nenhum `git.*` recebe diretório-alvo (rodam sobre o cwd do processo `mhl test`, igual `git.diff`) e `.mh` não tem `cd`/chdir, então o arquivo dependia do repositório onde `mhl test` fosse invocado ter pelo menos um commit alcançável: fràgil entre ambientes (clone raso, sandbox de CI) e redundante — `TestGitHandoffCycle` já prova o mesmo dispatch `.mh` de forma isolada e mais segura. Decisão: cobertura via `.mh` fica só nas Fases 0 e 2, que não precisam de isolamento de diretório.

## Fase 2 — Métodos de valor: string e array de ordem superior — ✅ Concluída

**Ajuste em relação ao sketch original:** a assinatura de `callValueMethod` só precisou
ganhar `depth int` (`eval.go:569`), não `ctx *evalCtx` — `invokeClosureWithValues` (nova,
extraída de `callClosure` em `closure.go`) recebe a closure e os argumentos já avaliados
e monta seu próprio `callCtx` a partir do `definingCtx` capturado pela closure, sem
precisar do `ctx` do chamador. Único call site atualizado: `eval.go:428`
(`applyTrailers`).

### 2.1 Strings (fecha ⚠️ path-safety guard, sanitização `OneLine`/truncamento) — ✅ Implementado

Novos cases em `callValueMethod`, todos recebendo `receiver.(string)`:
`split(sep)`, `replace(old, new)`, `contains(sub)`, `starts_with(prefix)`,
`ends_with(suffix)`, `trim()`, `to_upper()`, `to_lower()`, `substring(start, end)`
(bounds byte-based, mesmo estilo de `size()`). `contains` também aceita um array como
receiver (varre com `reflect.DeepEqual`, igual `index_of`).

### 2.2 Arrays de ordem superior (fecha ⚠️ seleção determinística por prioridade) — ✅ Implementado

- `filter(fn)` — `fn: (item) -> bool`, retorna novo array só com os itens que passam; erro se `fn` não retornar bool.
- `sort_by(fn)` — `fn: (item) -> number|string`, retorna novo array ordenado pela chave extraída (estável, `sort.SliceStable`, "decorate-sort-undecorate" — a closure roda uma vez por elemento, não por comparação); erro se as chaves misturarem tipos ou não forem number/string.
- `find(fn)` — primeiro item que passa em `fn`, ou `null`.

Confirmado reproduzindo `FeatureStore.NextPending()` em MHL puro (`test_higher_order_array.mh`, describe `deterministic_feature_selection`):
```
var ready = features
    .filter((f) -> f.status == "pending")
    .filter((f) -> f.dependsOn.filter((d) -> !passedIds.contains(d)).is_empty())
    .sort_by((f) -> f.priority)
var next = ready[0]
```

- **Teste:** `test/e2e/lang/syntax/test_string_methods.mh` (25 assertions) e
  `test_higher_order_array.mh` (18 assertions, incluindo a reprodução de
  `NextPending()` acima) — rodados via `mhl test e2e/lang/syntax` (alvo
  `functional-test` do `Makefile`).

### Achado durante a Fase 2: bug pré-existente no parser (fora de escopo)

Ao escrever `test_string_methods.mh`, um literal de string igual a **exatamente** `"-"`
ou `"!"` (nada mais) é mal interpretado sempre que aparece numa posição onde uma
expressão poderia começar (praticamente qualquer argumento de chamada ou lado direito de
atribuição): o parser (Participle) casa literais de gramática como `'-'`/`'!'` por
**valor do token**, não por **tipo**; como `participle.Unquote("String")`
(`internal/lang/parser/parser.go:20`) transforma o token String `"-"` (3 chars, com
aspas) em valor `-` (1 char) — colidindo com o literal do operador unário — o backtracking
de `UseLookahead(MaxLookahead)` passa a aceitar esse token como o operador `-`/`!` prefixo
em vez de como a string primária, engolindo a statement seguinte como operando. Ex.:
`var x = "-"` vira `var x = -<próxima expressão>`, e `f(" ", "-")` vira erro de parse
("unexpected token )"). Não afeta nenhum outro caractere de pontuação testado
(`:`, `,`, `.`, `+`, `*`, `=`) — só os dois que também são operadores unários prefixos.
Contornado nos testes desta fase evitando literais de string iguais a `"-"`/`"!"`
sozinhos. Não é um item deste plano (é bug de parser, não de cobertura de
`Flows.Development`) — decisão pendente do usuário se entra num fix separado.

## Fase 3 — Polimento de `fs` (fecha ⚠️ append de arquivo) — ✅ Concluída

- **Arquivo:** `internal/features/nativeops/fs.go` — `func Append(path, content string) (bool, error)` com `os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)`, mesma convenção de criar diretórios pais que `Write` já tem.
- **Dispatch:** `tool.go`, case `"fs.append"` (mesma forma de `fs.write`: `path`/`content` nomeados ou posicionais).
- **Teste:** `fs_test.go` (cria arquivo quando ausente, soma sem sobrescrever, cria diretórios pais) + `internal/cli/tool_test.go` (`TestToolFSAppendAddsWithoutOverwriting`, `TestToolFSAppendCreatesFileWhenMissing`, end-to-end via `.mh`) + cenário real em `test/e2e/features/test_memory.mh` (`memory_session_mem_value_appended_as_a_progress_line`: o caso de uso motivador — uma linha de `progress.txt` por feature completada, valor vindo de `memory`, lido de volta com `fs.read().trim().split("\n")`, compondo as Fases 0–3).
- **Prioridade baixa confirmada:** `fs.read` + `fs.write` já resolviam via read-concat-write; isso evita reescrever o arquivo inteiro a cada linha, mas não era bloqueio — só ganho de eficiência/atomicidade.

## Fase 4 — Verify em paralelo (fecha ⚠️ fan-out concorrente) — ✅ Concluída

- **Arquivo:** `internal/features/nativeops/cmd.go` — `func ExecAll(ctx, commands [][]string, timeout time.Duration) ([]map[string]any, error)`: dispara uma goroutine por comando (mesmo isolamento de processo que `Exec`/`ExecArgs` já têm — grupo de processo próprio via `tools.Exec`), `sync.WaitGroup`, devolve os resultados na mesma ordem de entrada (não a ordem de conclusão) — `results[i]` sempre corresponde a `commands[i]`. `timeout` é por comando, não do lote inteiro. Uma falha genuína de lançamento em qualquer comando (binário inexistente) aborta o lote inteiro com erro indexado (`cmd.exec_all[i]: ...`); exit code não-zero não é erro (mesma filosofia de `Exec`/`ExecArgs`).
- **Dispatch:** `tool.go`, case `"cmd.exec_all"`, com novo helper `callArgs.commandListNamedOrAt` — aceita array de comandos, nomeado (`commands:`) ou posicional, cada elemento string (split por espaço, mesmo caminho de `cmd.exec`) ou array de argv (verbatim, mesmo caminho de `cmd.exec`'s forma array) — reaproveita `toStringSlice` da Fase 0.2. `timeout` nomeado, aplicado por comando.
- **Teste:** `cmd_test.go` — `TestExecAllRunsCommandsConcurrently` prova paralelismo real (três `sleep 0.3` terminam bem abaixo da soma de 900ms), `TestExecAllPreservesInputOrderNotCompletionOrder` (um comando mais lento no índice 0 não embaralha a ordem), non-zero exit não é erro, falha de binário aborta o lote com índice identificado, lote vazio retorna resultado vazio. `internal/cli/tool_test.go`: `TestToolCmdExecAllRunsMixedCommandForms` (end-to-end via `.mh`, misturando forma string e array na mesma chamada — o cenário real do `TryParallelConfiguredVerify` do C#) e erro de argumento.
- **Confirmado:** baixa prioridade era a expectativa certa — semanticamente equivalente a rodar sequencial com `AND` sobre os `exit_code`; o ganho é só performance quando o número de comandos de verify cresce.

## Fase 5 — Nota de arquitetura (não é código)

O gap ❌ "persistir estado entre invocações separadas de processo" **não precisa de
mudança no runtime** se o flow for reestruturado como um único processo `mhl run` de
longa duração (`loop pipeline FeatureCycle { stop_when, max_iterations, ...steps }` chamando
`agent.run()` internamente para o LLM) — que é exatamente o que
`development_feature_cycle.mh` + `development_loop.mh` já prototipam. Isso substitui o
padrão do C# de "harness reinvocado a cada turno por um driver externo via stdin/stdout"
(`HarnessHost.Run`, `Program.cs:37`) por um processo único de ponta a ponta.
Só valeria revisitar isso se um caso de uso futuro exigir literalmente reiniciar o
processo mhl entre turnos — aí a saída é `memory { type: "json" }`, que já persiste em
disco entre invocações de `mhl run`.

## Sequenciamento e estimativa

| Fase | Item | Esforço | Status | Desbloqueia |
|---|---|---|---|---|
| 0.1 | `json.stringify` | Pequeno (1 função + 1 case) | ✅ Concluído | Fecha ❌ serialização |
| 0.2 | `cmd.exec` com array de args | Pequeno-médio (dispatch + normalização) | ✅ Concluído | Fecha ⚠️→❌ quoting |
| 1 | `git.add/commit/status/rev_parse/log` | Médio (5 funções + testes com repo real) | ✅ Concluído | Fecha ❌ git |
| 2.1 | Métodos de string | Médio (refatora assinatura de `callValueMethod`) | ✅ Concluído | Fecha ⚠️ path-safety/sanitização |
| 2.2 | `filter`/`sort_by`/`find` | Médio (reusa refator de 2.1 + `callClosure`) | ✅ Concluído | Fecha ⚠️ seleção por prioridade |
| 3 | `fs.append` | Pequeno | ✅ Concluído | Fecha ⚠️ append (baixa prioridade) |
| 4 | `cmd.exec_all` | Médio (concorrência) | ✅ Concluído | Fecha ⚠️ verify paralelo (baixa prioridade) |
| 5 | — | Nenhum (decisão de design) | ⬜ Pendente (não é código) | Fecha ❌ persistência entre processos |

**Fases 0–4 concluídas — plano de código fechado.** `json.stringify`, `cmd.exec` com
array de args, `git.add/commit/status/rev_parse/log` nativos, métodos de string
(`split`, `replace`, `contains`, `starts_with`, `ends_with`, `trim`, `to_upper`,
`to_lower`, `substring`), métodos de array de ordem superior (`filter`, `sort_by`,
`find`), `fs.append` e `cmd.exec_all` estão implementados e testados (unitário + e2e).
O ciclo `implement→verify→handoff` completo do C# porta para MHL sem gambiarra
nenhuma — incluindo seleção determinística de feature por prioridade+dependência,
registro de progresso estilo `progress.txt`, e verify multi-gate em paralelo
(lint/typecheck/tests fanning out e convergindo num AND, o padrão diamond do
`TryParallelConfiguredVerify`). **Fase 5** não é um item de implementação — é a
decisão de estruturar o flow como um `loop` mhl de processo único em vez de replicar
o `HarnessHost` stdio do C#; nenhum código pendente depende dela.
