# CENÁRIO-011 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-011-state-dir-restart.md`](CENARIO-011-state-dir-restart.md) não foi alterado.

## Cenário 011: Estado de run sobrevive a restart do processo (`--state-dir`)

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 13:31 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-011/mhl` (cópia de `sample/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Portas | caso A = 8731 · caso B (controle) = 8732 |
| Workflow | `DocPipeline` (`checkpoint { strategy: "per_step" }`), `repo:"billing"`, `approved:"no"` |
| Script | [`run.sh`](run.sh) |

## Caso A — COM `--state-dir D`

| Passo | Esperado | Obtido |
|---|---|---|
| proc1 `run/start DocPipeline {approved:"no"}` | `working` | `runId=4f834d17…`, `working` |
| proc1 `run/status` (antes de matar) | parada no gate | `state:"failed"`, `step:"Review"` |
| **matar proc1**, subir **proc2** no mesmo `D` | — | ✔ |
| proc2 `run/status { runId }` | `state:"failed"`, `step:"Review"`, `resumable:true` | `state:"failed"`, `step:"Review"`, `reached:["Draft"]`, `resumable:true` (reconstruída do disco) |
| proc2 `run/resume { runId, arguments:{approved:"yes"} }` | `working` → `completed` | `working` → `completed` |
| `vars` finais | `published == "published docs for billing (reviewed)"` | `published:"published docs for billing (reviewed)"`, `review:"reviewed"`, `reached:["Draft","Review","Publish"]` |

Logs dos dois processos:

```
[proc1] run started (resume:false) DocPipeline
        step: Draft / step: Review
        run failed  durationMs=1015  steps=2       ← gate fail()
        (processo morto)
[proc2] run started (resume:true)  DocPipeline     ← run/resume reconstruiu do disco
        step: Review / step: Publish
        run completed  steps=3
```

Owner na `proc1` = `a0b4a645…`; na `proc2` = `a1e973cb…` — sessões diferentes (cada processo
gera ids novos). Como o owner por hash-de-sessão **não** é persistido, o primeiro chamador
após o restart reivindica o `runId` (comportamento histórico, citado nas observações do cenário).

## Caso B (controle) — SEM `--state-dir`

| Passo | Esperado | Obtido |
|---|---|---|
| proc1 `run/start` → `run/status` | parada no gate | `state:"failed"`, `step:"Review"` |
| matar proc1, subir proc2 (dir por processo) | startup avisa | `note: no --state-dir/MHL_SERVE_STATE_DIR ... async run state is per-process and lost on restart` |
| proc2 `run/status { runId }` | `-32602 unknown runId` | `{"error":{"code":-32602,"message":"unknown runId \"cb05fa01…\""}}` |

## Evidências

- [x] [`logs/with-state-dir-server1.log`](logs/with-state-dir-server1.log), [`logs/with-state-dir-server2.log`](logs/with-state-dir-server2.log) — `run started`/`failed` (proc1) e `run started(resume)`/`completed` (proc2)
- [x] [`logs/with-state-dir-status-after.json`](logs/with-state-dir-status-after.json) — `failed`/`Review`/`resumable:true` pós-restart
- [x] [`logs/with-state-dir-resume.json`](logs/with-state-dir-resume.json), [`logs/with-state-dir-status-final.json`](logs/with-state-dir-status-final.json) — resume → `completed` com `vars`
- [x] [`logs/no-state-dir-server2.log`](logs/no-state-dir-server2.log) — aviso de estado por-processo
- [x] [`logs/no-state-dir-status-after.json`](logs/no-state-dir-status-after.json) — `-32602 unknown runId`
- [x] [`logs/client.log`](logs/client.log)

## Observações

- Referência de design: item 01 (gap 1 — estado node-local). Aqui é o **mesmo nó**, só um
  restart de processo — equivalente a um PVC no K8s (variante `deployment-durable.yaml`).
- Detalhe cosmético (não asserido): `reached:["Draft"]` e `stepIndex:0` no `run/status`
  reconstruído (só os passos concluídos do checkpoint são conhecidos); após o `run/resume`
  completar, `reached` fica `["Draft","Review","Publish"]`.
- Servidores encerrados ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** Com `--state-dir`, um `runId` iniciado por um processo é reconstruído do disco por
outro processo apontado para o mesmo diretório (`run/status` → `failed`/`Review`/`resumable`),
e `run/resume` o leva a `completed` com as `vars` corretas. Sem `--state-dir`, o mesmo fluxo
devolve `-32602 unknown runId` após o restart (e o startup avisa que o estado é por-processo).
