# Cenário 007: Ciclo de vida assíncrono (run/start · run/status · run/resume · run/cancel · run/list)

**Objetivo:** Verificar a superfície assíncrona do pod para um workflow longo com
gate humano: iniciar, acompanhar por polling, retomar após aprovação, listar e
cancelar — tudo num único processo.

```gherkin
Dado que o servidor MCP está em execução
Quando o cliente chama run/start para DocPipeline com approved="no"
Então recebe um runId e a run avança Draft -> Review e para (failed, resumable)
Quando o cliente chama run/resume com approved="yes"
Então a run vai a Publish e termina como completed com vars.published preenchido
E run/list mostra a run do próprio caller
E uma sessão diferente recebe "unknown runId" ao consultar essa run
E run/cancel numa run SlowBuild em andamento a leva a canceled
```

**Resultado Esperado:**
- `run/start` → `{ runId, state: "working" }` (ou `queued` → `working`).
- `run/status` em loop → `state: "failed"`, `step: "Review"`, `reached: ["Draft","Review"]`, `resumable: true`.
- `run/resume { runId, arguments: { approved: "yes" } }` → `state: "working"` e depois
  `state: "completed"`, `reached: ["Draft","Review","Publish"]`,
  `vars.published == "published docs for async-demo (reviewed)"`.
- `run/list` da sessão dona → contém o `runId`.
- `run/status`/`run/list` de **outra** sessão → `-32602 unknown runId` / `{"runs":[]}`.
- `run/cancel` numa run `SlowBuild` `working` → `state: "canceled"`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP (`step:` de cada estágio)
- [ ] Respostas de run/start, cada run/status do polling, run/resume, run/list, run/cancel

### Observações:
- Referência de design: itens 01 e 04. Um pod só — não exercita afinidade entre pods.
- Modo **handshake** (`initialize` → `Mcp-Session-Id`): cada `initialize` é um owner
  distinto, o que permite testar o isolamento por sessão.
- `DocPipeline` declara `checkpoint { strategy: "per_step" }` — é isso que torna o
  gate `fail()` retomável.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
