# CENÁRIO-009 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-009-drain-shutdown.md`](CENARIO-009-drain-shutdown.md) não foi alterado.

## Cenário 009: Drain gracioso no SIGTERM e checkpoint de shutdown

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 13:06 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-009/mhl` (cópia de `sample/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` (já com o fix do drain — `http.go` usa `context.WithoutCancel`) |
| Script | [`run.sh`](run.sh) |

## P1 — `--drain-timeout 20s` (`mhl serve mcp --http --addr 127.0.0.1:8728 --token … --drain-timeout 20s --state-dir <D>`)

| Verificação | Esperado | Obtido |
|---|---|---|
| `run/start SlowBuild` | `working` | `state:"working"`, `runId=4ab896e5…` |
| Durante o dreno: `GET /readyz` | `503` `draining` | **503** (corpo `draining`) |
| Durante o dreno: `GET /healthz` | `200` `ok` | **200** (corpo `ok`) |
| Durante o dreno: `run/start` | HTTP `503`, `-32000` | `{"error":{"code":-32000,"message":"server is draining — not accepting new runs"}}` |
| Processo espera a run antes de encerrar | delete→sumiço ≥ ~4 s, ≤ 20 s | **6 s** após o SIGTERM (rc=0) |
| stderr linha JSON `draining` | `timeout":"20s"` | `{"...","msg":"draining","timeout":"20s","working":1}` |

Log do servidor P1 ([`logs/mcp-server.log`](logs/mcp-server.log)):

```
run started  SlowBuild  4ab896e5…
  step: Compile
draining     timeout=20s  working=1        ← SIGTERM chegou aqui
  step: Package                             ← a run CONTINUOU
  step: Ship
run failed   durationMs=7038  steps=3      ← terminou sozinha (timeout do Ship)
                                            ← só então o processo saiu (6 s pós-SIGTERM)
```

## P2 — restart no mesmo `--state-dir` → checkpoint de shutdown

`run/status { runId }` no processo novo ([`logs/run-status-after-restart.json`](logs/run-status-after-restart.json)):

```json
{ "state": "failed", "step": "Ship", "reached": ["Compile","Package"],
  "resumable": true, "runId": "4ab896e5…", "tool": "SlowBuild" }
```

`resumable: true` mesmo `SlowBuild` declarando apenas `checkpoint { enabled: true }` (sem
`strategy: "per_step"`) — o processo deixou um ponto de retomada em disco ao terminar.

## P3 — `--drain-timeout 0` → cancelamento imediato

| Verificação | Esperado | Obtido |
|---|---|---|
| stderr | `"cancelImmediately":true` | `{"...","msg":"draining","timeout":"0s","cancelImmediately":true}` |
| Tempo SIGTERM → saída do processo | ~5 s | **0 s** |

## Evidências

- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — P1: `Compile` → `draining(working:1)` → `Package`/`Ship` → `run failed` (7038 ms)
- [x] [`logs/readyz-draining.txt`](logs/readyz-draining.txt) (`draining`) / [`logs/healthz-draining.txt`](logs/healthz-draining.txt) (`ok`)
- [x] [`logs/run-start-during-drain.json`](logs/run-start-during-drain.json) — `-32000 server is draining`
- [x] [`logs/run-status-after-restart.json`](logs/run-status-after-restart.json) — `failed` / `resumable:true` pós-restart
- [x] [`logs/mcp-server-drain0.log`](logs/mcp-server-drain0.log) — P3 `cancelImmediately:true`
- [x] [`logs/client.log`](logs/client.log) — trilha completa (tempos medidos)

## Observações

- Referência de design: item 07 ("Shutdown cancels in-flight runs after 5s") — agora com
  `--drain-timeout` honrado.
- Este é o **cross-check do achado do CENÁRIO-K8S-001**: aquele achado (`--drain-timeout`
  virava no-op porque `runsCtx` herdava o cancelamento do contexto de sinal) foi corrigido
  em `internal/mcpserver/http.go` (`context.WithCancel(context.WithoutCancel(ctx))`), e aqui,
  no host, a run também sobrevive ao SIGTERM e o processo a aguarda.
- Detalhe cosmético (não asserido): `stepIndex:0` no `run/status` da run reconstruída pós-restart.
- Servidores encerrados ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** No SIGTERM com `--drain-timeout 20s`: `/readyz` vira `503` (e `/healthz` fica `200`),
`run/start` é recusado com `-32000 server is draining`, a run em andamento **continua e termina
sozinha** (o processo só sai 6 s depois), e o stderr registra a linha `draining` com o timeout.
Após restart no mesmo `--state-dir` a run é `resumable` (checkpoint de shutdown sem `per_step`).
Com `--drain-timeout 0` o SIGTERM encerra na hora (`cancelImmediately:true`).
