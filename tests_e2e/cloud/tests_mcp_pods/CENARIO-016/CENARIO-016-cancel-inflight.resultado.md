# CENÁRIO-016 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-016-cancel-inflight.md`](CENARIO-016-cancel-inflight.md) não foi alterado.

## Cenário 016: `run/cancel` aborta uma chamada em andamento

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 14:52 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-016/mhl` (cópia de `tests/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8737 --token <gerado> --state-dir <tmp> tests/cloud` |
| Workflow | `SlowBuild` — `Compile` = `cmd.exec(["sh","-c","sleep 3; ..."])` |
| Script | [`run.sh`](run.sh) |

## Passos e resultados

| Passo | Esperado | Obtido |
|---|---|---|
| `run/start SlowBuild` | `working` | `runId=f2cfcaad…`, `state:"working"` |
| `run/cancel` (~1 s após o start) | `state:"canceled"` imediato | `state:"canceled"` a **t+1,05 s** do start |
| `run/status` imediato | `canceled`, `reached:["Compile"]` | `state:"canceled"`, `step:"Compile"`, `reached:["Compile"]` |
| Observar ~4 s: `run/logs` / stderr **não** mostram `step: Package` | run não avança | `run/logs.text = "session: f2cfcaad…\nstep: Compile\n"` — **sem `step: Package`** |
| `run/status` a t+~5 s | ainda `canceled`, `reached:["Compile"]` | idem |
| stderr do servidor | `step: Compile` presente, `step: Package` ausente | ✔ |

## Evidência do abort do subprocesso ([`logs/mcp-server.log`](logs/mcp-server.log))

```
14:52:42.334  run started   SlowBuild  f2cfcaad…
              step: Compile                       ← cmd.exec ["sh","-c","sleep 3; ..."]
14:52:43.435  run canceled  durationMs=1101  steps=1
```

`durationMs=1101` (≈ o ~1 s até o `run/cancel`), **não** os ~3 s que o `sleep 3` do
`Compile` levaria se fosse aguardado — o subprocesso `sh` foi cortado no meio. E nos ~4 s
seguintes a run **não** entrou em `Package` (nem no `run/logs` nem no stderr).

## Evidências

- [x] [`logs/run-start.json`](logs/run-start.json), [`logs/run-cancel.json`](logs/run-cancel.json) (`canceled`/`reached:["Compile"]`)
- [x] [`logs/run-status-immediate.json`](logs/run-status-immediate.json), [`logs/run-status-after.json`](logs/run-status-after.json) — `canceled` antes e depois dos 4 s de observação
- [x] [`logs/run-logs.json`](logs/run-logs.json) — só `session:` + `step: Compile`
- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — `run started` → `step: Compile` → `run canceled` (`durationMs:1101`), sem `step: Package`
- [x] [`logs/client.log`](logs/client.log) — timestamps do start/cancel

## Observações

- Referência de design: item 01, direção ("a run-level cancel also aborts a blocking call
  already in flight — an agent subprocess / cmd/git/http native op"). Confirmado para `cmd.exec`.
- Detalhe (não asserido): `run/status` da run cancelada vem com `resumable:true` — o
  `SlowBuild` declara `checkpoint { enabled: true }`, então há um ponto de retomada em disco.
- Servidor encerrado ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** `run/cancel` durante o passo `Compile` derruba o subprocesso `sh` na hora
(`durationMs ≈ 1101`, não ~3000), `run/status` fica `canceled` imediatamente e nos segundos
seguintes a run **não avança** para `Package` — nem no `run/logs` nem no stderr do servidor.
