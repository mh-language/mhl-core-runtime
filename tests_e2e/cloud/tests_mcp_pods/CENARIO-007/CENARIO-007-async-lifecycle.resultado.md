# CENÁRIO-007 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-007-async-lifecycle.md`](CENARIO-007-async-lifecycle.md) não foi alterado.

## Cenário 007: Ciclo de vida assíncrono (run/start · run/status · run/resume · run/cancel · run/list)

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 12:50 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-007/mhl` (cópia de `tests/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8726 --token <gerado> --state-dir <tmp> tests/cloud` |
| Modo | handshake (`initialize` → `Mcp-Session-Id`) — sessão A (dona) e sessão B (outro owner) |
| Script | [`run.sh`](run.sh) |

## Passos e resultados

| Passo | Esperado | Obtido |
|---|---|---|
| `run/start DocPipeline {repo:"async-demo", approved:"no"}` | `runId`, `state:"working"` | `runId=9e7f87f0…`, `state:"working"` |
| `run/status` (poll #1) | avança | `state:"working"`, `step:"Draft"`, `reached:["Draft"]` |
| `run/status` (poll #2) | para no gate | `state:"failed"`, `step:"Review"`, `reached:["Draft","Review"]`, `resumable:true`, `error:"...awaiting documentation review for async-demo"` |
| `run/resume {runId, arguments:{approved:"yes"}}` | `state:"working"` | `state:"working"` |
| `run/status` (pós-resume) | `state:"completed"` | `state:"completed"`, `reached:["Draft","Review","Publish"]` |
| `vars` finais | `published == "published docs for async-demo (reviewed)"` | `published: "published docs for async-demo (reviewed)"`, `review: "reviewed"`, `draft: "draft docs for async-demo\n"` |
| `run/list` sessão A (dona) | contém o `runId` | 1 run, `runId=9e7f87f0…`, `state:"completed"` |
| `run/list` sessão B | `{"runs":[]}` | `{"runs":[]}` |
| `run/status` sessão B `{runId de A}` | `-32602 unknown runId` | `{"error":{"code":-32602,"message":"unknown runId \"9e7f87f0…\""}}` |
| `run/start SlowBuild` + `run/cancel` | `state:"canceled"` | `run/cancel` → `canceled`; `run/status` seguinte → `canceled` |

## Evidência do fluxo no log do servidor ([`logs/mcp-server.log`](logs/mcp-server.log))

```
run started (resume:false)  DocPipeline
  step: Draft
  step: Review
run failed  durationMs:1014  steps:2          ← gate fail()
run started (resume:true)   DocPipeline        ← run/resume
  step: Review
  step: Publish
run completed  steps:3
run started (resume:false)  SlowBuild
  step: Compile
run canceled  durationMs:1058  steps:1         ← run/cancel durante o Compile
```

## Evidências

- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — `run started`/`failed`/`started(resume)`/`completed`/`canceled` + `step:` de cada estágio
- [x] [`logs/run-status-poll.log`](logs/run-status-poll.log) — cada `run/status` do polling (working→failed)
- [x] [`logs/run-start.json`](logs/run-start.json), [`logs/status-parked.json`](logs/status-parked.json), [`logs/run-resume.json`](logs/run-resume.json), [`logs/status-final.json`](logs/status-final.json)
- [x] [`logs/run-list-A.json`](logs/run-list-A.json), [`logs/run-list-B.json`](logs/run-list-B.json), [`logs/run-status-B.json`](logs/run-status-B.json)
- [x] [`logs/slow-start.json`](logs/slow-start.json), [`logs/slow-cancel.json`](logs/slow-cancel.json), [`logs/slow-status.json`](logs/slow-status.json)
- [x] [`logs/client.log`](logs/client.log)

## Observações

- Isolamento por sessão (sem `--principal-header`): o `owner` da run é o hash da sessão A;
  a sessão B, com outro `Mcp-Session-Id`, não vê nem acessa a run.
- `DocPipeline` declara `checkpoint { strategy: "per_step" }` — é o que torna o gate `fail()`
  retomável pelo `run/resume`.
- Detalhe cosmético (não é falha, o cenário não asserta): no `run/status` da run **completed**,
  `stepIndex` vem `2` com `step:"Publish"` (que é o passo 3). O `stepIndex` reflete a última
  chamada de `OnStep` da leg de resume, que reentrou em `Review`. `reached` e `state` estão certos.
- Servidor(es) encerrado(s) ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** O ciclo assíncrono completo funciona num pod: `run/start` → polling de
`run/status` até o gate (`failed`/`resumable`) → `run/resume` com a aprovação → `completed`
com as `vars` corretas; `run/list` isolado por caller (outra sessão → `{"runs":[]}` /
`-32602 unknown runId`); `run/cancel` numa run `working` a leva a `canceled`.
