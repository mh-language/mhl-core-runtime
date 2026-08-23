# Flows.Development (C#) → mhl: cobertura atual

Comparação entre `dotnet/Flows.Development/` (harness C# do flow de desenvolvimento
longo-executando) e as capacidades atuais da linguagem/runtime mhl
(`src/mhl-runtime`), para avaliar o quanto do flow já dá para reimplementar em mhl.

## Tabela objetiva

| Área | Necessidade do C# | Status | Nota |
|---|---|---|---|
| State machine (bearings→implement→verify→handoff) | `DevelopmentTasks.cs` | ✅ Atendido | já prototipado em `development_feature_cycle.mh` com `step`/`goto`/`break` |
| Loop "uma feature por sessão" | `Program.cs` + `HarnessHost.Run` | ✅ Atendido | `loop pipeline X { stop_when, max_iterations }` |
| Retry local + escalação (3 falhas → replan) | `HandleVerifyFailure` | ✅ Atendido | contador em `memory`, `goto`/`break` |
| Rodar comando de verify simples (`dotnet test`) | `TryConfiguredVerify` | ✅ Atendido | `cmd.exec(cmd, timeout: Ns)` → `{stdout, stderr, exit_code}` |
| Timeout + kill de árvore de processo | `RunProcess` (Kill entireProcessTree) | ✅ Atendido | grupo de processo + SIGKILL já existe (`process_unix.go`) |
| Ler env var de config | `ExternalOrArg` | ✅ Atendido | `env("...")` nativo |
| Retry/backoff/fallback/cache na chamada ao LLM | *(não existe no C#)* | ✅ Atendido (mhl vai além) | `agent{retry, fallback, cache}` — C# não tem isso |
| Ler/escrever arquivo inteiro (progress.txt, feature_list.json) | `File.ReadAllText`/`WriteAllText` | ✅ Atendido (implementado) | `fs.read`/`fs.write` para arquivo inteiro; `fs.append` (novo) para progress.txt-style sem reler o arquivo — ver [gap-plan.md, Fase 3](flows-development-mhl-gap-plan.md#fase-3--polimento-de-fs-fecha-️-append-de-arquivo--✅-concluída) |
| Comando com args citados (`git commit -m "msg com espaço"`) | `TokenizeCommand` | ✅ Atendido (implementado) | `cmd.exec` agora aceita array de argv (`cmd.exec(["git","commit","-m",msg])`), sem passar por `strings.Fields` — ver [gap-plan.md, Fase 0.2](flows-development-mhl-gap-plan.md#fase-0--fundação-desbloqueia-tudo-abaixo) |
| Verify commands em paralelo (`Task.WaitAll`) | `TryParallelConfiguredVerify` | ✅ Atendido (implementado) | `cmd.exec_all([...], timeout: Ns)` — fan-out real por goroutine, resultados na ordem de entrada — ver [gap-plan.md, Fase 4](flows-development-mhl-gap-plan.md#fase-4--verify-em-paralelo-fecha-️-fan-out-concorrente--✅-concluída) |
| Seleção determinística por prioridade + dependência pronta | `FeatureStore.NextPending()` | ✅ Atendido (implementado) | `array.filter(fn)`/`array.sort_by(fn)` com lambdas — ver [gap-plan.md, Fase 2.2](flows-development-mhl-gap-plan.md#22-arrays-de-ordem-superior-fecha-️-seleção-determinística-por-prioridade--✅-implementado) |
| Path-safety guard (root/home/harness dir) | `ResolveTargetDir` | ✅ Atendido (implementado) | `string.split/replace/contains/starts_with/...` — ver [gap-plan.md, Fase 2.1](flows-development-mhl-gap-plan.md#21-strings-fecha-️-path-safety-guard-sanitização-oneline-truncamento--✅-implementado) |
| Sanitização (`OneLine`, truncamento UTF-8 seguro) | `TruncateUtf8Bytes`, `OneLine` | ✅ Atendido (implementado) | idem — `trim()`/`substring()`/`to_upper()`/`to_lower()` cobrem o essencial |
| Git: add / commit / status / rev-parse / log | `GitCommand.Run` | ✅ Atendido (implementado) | `git.add/commit/status/rev_parse/log` nativos — ver [gap-plan.md, Fase 1](flows-development-mhl-gap-plan.md#fase-1--git-nativo-fecha-o-gap--git-addcommitstatusrev-parselog--✅-concluída) |
| Serializar objeto/array de volta para JSON | `FeatureStore.Write` (reserializa a cada mudança) | ✅ Atendido (implementado) | `json.stringify(value)` adicionado, espelha `json.parse` — ver [gap-plan.md, Fase 0.1](flows-development-mhl-gap-plan.md#fase-0--fundação-desbloqueia-tudo-abaixo) |
| Persistir estado entre invocações separadas de processo | `state.json` sobrevivendo a "fresh-context sessions" | ❌ Gap (se replicar a arquitetura 1:1) | `var` de step some a cada execução; só `memory{type:"json"}` sobrevive — resolvido se você reestruturar como um `loop` único de longa duração (não replicar o `HarnessHost` stdio) |
| Funções de string em geral (split/replace/upper/lower) | várias | ✅ Atendido (implementado) | `split/replace/contains/starts_with/ends_with/trim/to_upper/to_lower/substring` — ver [gap-plan.md, Fase 2.1](flows-development-mhl-gap-plan.md#21-strings-fecha-️-path-safety-guard-sanitização-oneline-truncamento--✅-implementado) |

## Inventário atual de nativeops (para referência)

`src/mhl-runtime/internal/features/nativeops/`:

- `cmd.Exec(command string, timeout time.Duration)` — `cmd.go:29`
- `cmd.ExecArgs(argv []string, timeout time.Duration)` — `cmd.go:55` — **novo (Fase 0.2)**, argv direto, sem `strings.Fields`
- `cmd.ExecAll(commands [][]string, timeout time.Duration)` — `cmd.go` — **novo (Fase 4)**, fan-out concorrente (uma goroutine por comando), resultados na ordem de entrada
- `fs.Read(path)` / `fs.Write(path, content)` — `fs.go:10,21`
- `fs.Append(path, content)` — `fs.go` — **novo (Fase 3)**, `O_APPEND`, cria arquivo/diretórios pais se ausentes
- `git.Diff(target)` — `git.go:17`
- `git.Add(paths []string)` / `git.Commit(message string, paths []string)` / `git.Status(paths []string)` / `git.RevParse(ref string)` / `git.Log(n int)` — `git.go` — **novos (Fase 1)**
- `http.Post(url, headers, body)` — `http.go:25`
- `json.Parse(text)` — `json.go:14`
- `json.Stringify(value)` — `json.go:23` — **novo (Fase 0.1)**, inverso de `Parse`

No dispatch de linguagem (`internal/engine/interpreter/tool.go`), `cmd.exec(...)` agora
aceita tanto uma string (split por espaço, comportamento antigo) quanto um array de
strings (`cmd.exec(["git","commit","-m",msg])`, roteado para `ExecArgs`) — detectado
automaticamente pelo tipo do primeiro argumento. `cmd.exec_all([...], timeout: Ns)`
aceita um array de comandos, cada um string ou array de argv (mesma normalização),
rodando todos em paralelo e devolvendo os resultados na ordem de entrada. `git.add`/
`git.commit`/`git.status` aceitam `paths` opcional (nomeado ou posicional);
`git.rev_parse`/`git.log` retornam string crua (mesmo estilo de `git.diff`).

Métodos de valor (string/array), diferente de nativeops, vivem em `callValueMethod`
(`internal/engine/interpreter/eval.go`) e são chamados como `valor.metodo(...)` — mesmo
mecanismo de `size()`/`index_of()`/`keys()` já existentes:

- **String:** `split(sep)`, `replace(old,new)`, `contains(sub)`, `starts_with(prefix)`,
  `ends_with(suffix)`, `trim()`, `to_upper()`, `to_lower()`, `substring(start,end)` — **novos (Fase 2.1)**
- **Array de ordem superior:** `filter(fn)`, `sort_by(fn)`, `find(fn)`, onde `fn` é uma
  lambda `(item) -> expr` — **novos (Fase 2.2)**, usam o suporte a closures de primeira
  classe que o runtime já tinha (`internal/engine/interpreter/closure.go`)

## Onde começar

Plano detalhado e faseado em [flows-development-mhl-gap-plan.md](flows-development-mhl-gap-plan.md).
Status atual:

1. ✅ **`json.stringify`** — implementado (Fase 0.1).
2. ✅ **`cmd.exec` com args em array** — implementado (Fase 0.2).
3. ✅ **`git.add` / `git.commit` / `git.status` / `git.rev_parse` / `git.log`** nativos —
   implementado (Fase 1).
4. ✅ **Funções básicas de string** (Fase 2.1) e **`filter`/`sort_by`/`find` em array**
   (Fase 2.2) — implementado.
5. ✅ **`fs.append`** (Fase 3) — implementado.
6. ✅ **`cmd.exec_all`** (Fase 4) — implementado.

**Plano de código fechado (Fases 0–4).** Todo o ciclo `implement→verify→handoff` do
C# — seleção determinística de feature por prioridade/dependência com múltiplas
features reais, registro de progresso incremental, commit real com mensagem contendo
espaço, e verify multi-gate em paralelo (lint/typecheck/tests fanning out e
convergindo num AND) — porta para MHL sem gambiarra nenhuma. Só resta a Fase 5, que
não é código: é a decisão de estruturar o flow como um `loop` mhl de processo único
(como `development_feature_cycle.mh`/`development_loop.mh` já prototipam) em vez de
replicar o `HarnessHost` stdio do C#.
