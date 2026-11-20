# Nota de design: um workflow chamando outro (subprocesso)

**Status:** proposta, não implementada · **Data:** 2026-11-20 · inclui `--from <stage>` (seção 6)
**Contexto:** a fase D do estudo sobre agrupamento e DAG no mhl studio. As fases A (visão do
workflow `partial` inteiro), B (`stage`) e C (gates tipados, `pause(..., decision: T)`) já
estão na linguagem. Esta nota registra o que falta decidir antes de escrever código para D.

---

## 1. O que se quer

Um step que roda outro workflow como uma unidade, com contrato tipado e espera pelo
resultado. É o equivalente a:

- um template `dag`/`steps` chamado por outro, ou `templateRef` para um `WorkflowTemplate`,
  no Argo;
- um SMART Folder ou Sub-folder no Control-M;
- `states:startExecution.sync` no Step Functions.

```mh
type RoundIn = { repo: string, feedback: string }
type RoundOut = { status: string, attempts: number }

loop workflow ImplementationRound(req: RoundIn): RoundOut max 3 { ... }

workflow Story {
    step Implement {
        self.round = ImplementationRound.run(inputs: {repo: repo, feedback: feedback})
    }
}
```

O caso que motiva a proposta é o AgileSpec: o ciclo implementar → verificar → revisar →
corrigir, com `max_attempts`, é um subprocesso que hoje é escrito à mão com `goto` e
contadores. Com D, ele vira um `loop workflow ... max 3` com assinatura tipada.

Hoje `Name.run(inputs:)` só existe dentro de `test`. O comentário em
`internal/engine/interpreter/test_run.go` diz o motivo: rodar um workflow dentro de um step
deixa sem resposta o que fazer com checkpoint, `context` e recursão. As seções abaixo são
essas respostas.

## 2. Semântica proposta

| Tema | Proposta |
|---|---|
| Chamada | `W.run(inputs: {...})` passa a valer em qualquer step de workflow, não só em `test`. Devolve a projeção `output:` de `W` (checada contra `Out` quando `W` é tipado). |
| Falha do filho | Vira falha do step que chamou, como um `fail()`, com o `StepError` do filho encadeado. |
| `break` no filho | É um fim normal do filho: a chamada devolve a projeção. |
| Pausa no filho | **O pai pausa também.** O `awaiting` do pai aponta para o gate do filho e acrescenta `path: ["Implement", "ImplementationRound", "Review"]`. A retomada chega ao gate mais interno. |
| Cancelamento | Em cascata: cancelar o pai cancela o filho no próximo limite de step. |
| Checkpoint | O checkpoint do filho fica **dentro** do checkpoint do step do pai (`Checkpoint.Child`). Retomar o pai retoma o filho de onde parou, sem refazer os steps que o filho já concluiu. |
| Digest | O `DefinitionDigest` do pai passa a incluir o digest de cada workflow que ele chama, **só quando chama algum**. Um programa sem subprocesso mantém o digest de hoje. |
| Recursão | Limite de profundidade fixo (sugestão: 8), com erro claro. Recursão direta (`W` chama `W`) é rejeitada pelo lint. |
| `context` / sessão | O filho roda na sessão do pai, com `context.parent = {workflow, step}`. Não ganha um `context.vars` próprio. |
| Hooks | Os hooks do filho rodam normalmente. O `step_start` do pai vê o step que chama, não os steps internos do filho. |

## 3. Pontos que mudam formato (compatibilidade)

1. **Checkpoint v3.** O estado do filho aninhado exige `StateSchemaVersion` 3.
   - Um runtime novo lê checkpoints v2.
   - **Um runtime antigo não lê um checkpoint v3**, e já recusa com
     `ErrCheckpointSchemaTooNew`.
   - O risco está em `serve mcp --http` com várias réplicas sobre um `extension state`
     compartilhado, numa atualização gradual: uma réplica antiga pode receber um
     `run/resume` de um run iniciado por uma nova. Isso precisa de uma regra de deploy
     (atualizar todas as réplicas antes de liberar workflows com subprocesso), ou de uma flag
     `--allow-subworkflows` desligada por padrão durante a transição.
2. **`run/status`.** O campo `step` continua sendo uma string (o step do pai). O caminho até
   o step mais interno vai num campo novo, `path`. Nenhum cliente MCP quebra.
3. **Eventos `jsonl`.** Os steps do filho aparecem com `parent: "Implement"`, um campo novo.
   Um consumidor que ignore campos desconhecidos não é afetado.
4. **`mhl graph`.** Um step que chama outro workflow ganha `Node.subworkflow: "Name"`. O
   studio desenha esse nó como um subprocesso recolhido, que abre o diagrama do filho com
   duplo clique.

## 4. Decisões em aberto

1. **Só o mesmo programa, ou também workflows de outro diretório/pacote?** O `templateRef`
   do Argo permite as duas coisas. Para começar, a sugestão é só o mesmo programa
   (declarações importadas incluídas); "pacote" depende do modelo de distribuição de
   workflows, que ainda não existe.
2. **Filho assíncrono (`spawn W.run(...)`) já na primeira versão?** Sugestão: não. A chamada
   síncrona cobre o caso do AgileSpec, e o assíncrono traz de volta a questão de
   paralelismo e cancelamento.
3. **Gate dentro de `loop workflow`.** Hoje é recusado pelo lint (fase C). Um subprocesso
   `loop` com gate precisa disso: o `LoopCheckpoint` teria de registrar o gate, como o
   `Checkpoint` já faz. Vale resolver antes de D, porque o caso que motiva D é exatamente um
   loop com revisão humana.
4. **Saída parcial de um filho pausado.** O pai pausado expõe as variáveis parciais do
   filho? Sugestão: não, só `awaiting` e `path`, para não vazar estado intermediário pelo
   `run/status` do pai.

## 5. Plano sugerido

1. Gate em `loop workflow` (decisão 3): registrar o gate no `LoopCheckpoint` e liberar no
   lint. Mudança pequena, sem formato novo.
2. `W.run` síncrono em steps, **sem** retomada aninhada: um filho que pausa faz o pai falhar
   com uma mensagem clara. Já dá o contrato tipado e o grafo, sem checkpoint v3.
3. Checkpoint v3 com `Checkpoint.Child`: pausa e retomada propagadas, `path` em
   `awaiting`/`run/status`, digest composto.
4. Studio: nó de subprocesso e navegação para o diagrama do filho.

Cada passo pode ser lançado sozinho. Só o passo 3 tem o risco de compatibilidade da seção 3.

---

## 6. Relacionado: retomar a partir de uma fase (`--from <stage>`)

Não depende do subprocesso. Pode sair antes, e é pequeno. Entrou nesta nota porque é a
outra metade de "fase como unidade": D trata a fase como algo que se chama, e esta seção
trata a fase como algo que se refaz.

### O que se quer

Hoje o `--resume` só continua de onde o run parou: o step pausado ou que falhou, nunca o
começo da etapa. O Control-M tem "rerun folder", e o Argo tem "retry from node". O
equivalente aqui é refazer uma fase inteira de um run parado:

```sh
mhl run story.mh --session s1 --resume --from Specification
```

e, no MCP, `run/resume {runId, from: "Specification"}`.

### Semântica proposta

| Tema | Proposta |
|---|---|
| Alvo | Só o **nome de um stage**, nunca de um step. Pelo mesmo argumento da regra de `goto` entre etapas: a fase decide por onde começa, e quem chama depende só do nome dela. |
| Entrada | O primeiro membro do stage (`Pipeline.StageEntry`), como `goto <stage>`. |
| Que runs | Um run parado (pausado, falhou ou cancelado) que ainda tem checkpoint. Um run concluído não tem checkpoint e não se refaz assim. |
| Variáveis | As do checkpoint, ou seja, o estado no momento da parada. Não há rollback: o que a fase já escreveu continua lá. Por isso a fase precisa ser re-executável, a mesma regra de "at-least-once" que vale para qualquer step. |
| Inputs | Mesclados como em qualquer resume (`--input`, `arguments`). |
| Gate pendente | Descartado (`Checkpoint.Gate` vai para nil): a fase vai rodar de novo e chegará ao gate outra vez. `--from` junto com `--decision` é recusado. |
| Digest | A mesma checagem do resume. `--force` continua sendo a saída explícita. |
| Saída | `skipped` lista os steps concluídos antes da fase. A nova perna aparece nos eventos com o `stage` de cada step, como hoje. |

### Implementação (esboço)

- `execsvc.Request.From`: o nome é validado contra `pipeline.StageEntry` antes de qualquer
  hook, e um nome desconhecido dá `invalid_inputs` (ou um novo `invalid_from`). O checkpoint
  é lido antes, como já acontece com a decisão do gate (`Runner.Checkpoint`).
- O `Runner` ganha uma opção para entrar por outro step na retomada (`NextStep` sobrescrito
  e `Gate` limpo). **Não muda o formato do checkpoint**: a próxima gravação já sai normal.
- CLI: `--from <stage>`, só com `--resume`. MCP: `from` em `run/resume` e na ferramenta
  `mhl_run_resume`. O manifest `mhl://workflow/<name>` já lista os stages.
- Studio: ação "Refazer esta fase" na caixa do stage quando o run está parado.

### Decisão em aberto

**Variáveis do começo da fase, ou da parada?** Restaurar o estado do momento em que a fase
começou seria mais limpo, porque a fase rodaria de novo sobre a mesma entrada. Mas exigiria
um snapshot por fase no checkpoint (que cresce) e só valeria para fases que o run já
atravessou com o recurso ativo. Sugestão: começar com as variáveis da parada, que é o
comportamento previsível de "resume", e avaliar o snapshot se aparecer um caso real.
