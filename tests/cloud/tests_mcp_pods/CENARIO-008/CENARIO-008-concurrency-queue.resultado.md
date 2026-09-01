# CENÁRIO-008 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-008-concurrency-queue.md`](CENARIO-008-concurrency-queue.md) não foi alterado.

## Cenário 008: Concorrência limitada e fila de runs

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 12:57 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-008/mhl` (cópia de `tests/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8727 --token <gerado> --max-concurrent-runs 1 --state-dir <tmp> tests/cloud` |
| Modo | handshake (`initialize` → `Mcp-Session-Id`) |
| Script | [`run.sh`](run.sh) |

## Passos e resultados

| Passo | Esperado | Obtido |
|---|---|---|
| `run/start` #1 (SlowBuild t1) | `state:"working"` | `state:"working"` (sem `queuePosition`) — `runId=19fe9d6a…` |
| `run/start` #2 (SlowBuild t2) | `state:"queued"`, `queuePosition:0` | `state:"queued"`, `queuePosition:0` — `runId=93fab1c2…` |
| `run/start` #3 (SlowBuild t3) | `state:"queued"`, `queuePosition:1` | `state:"queued"`, `queuePosition:1` — `runId=6dd72cef…` |
| `GET /metrics` durante a fila | `runs_queued >= 1` | `mhl_serve_runs_active 1`, `mhl_serve_runs_queued 2` |
| `tools/call` SlowBuild com o pool cheio | após ~5 s: `-32000` / HTTP 503 | HTTP 503, `{"error":{"code":-32000,"message":"server at capacity — retry, or use run/start"}}` (levou ~5 s) |
| `run/cancel` na run #3 (queued) | `state:"canceled"`, sem executar | `state:"canceled"`; no log do servidor a run #3 só tem `run queued` — **nenhum `session:` / `step:`** |
| Ao terminar a run #1, a #2 sai da fila | `queued` → `working` | poll: `queued` → `queued` → **`working`** (às 12:57:39, logo após o `run failed` da #1) |

## Evidência da fila no log do servidor ([`logs/mcp-server.log`](logs/mcp-server.log))

```
run started  SlowBuild  19fe9d6a…            ← #1 pega o único slot
  step: Compile
run queued   SlowBuild  93fab1c2…            ← #2 enfileirada
run queued   SlowBuild  6dd72cef…            ← #3 enfileirada
  step: Package
  step: Ship
run failed   19fe9d6a…  durationMs:7033      ← #1 termina (timeout do Ship)
run started  SlowBuild  93fab1c2…            ← #2 assume o slot na hora
  step: Compile
```

A run #3 (`6dd72cef…`) aparece só como `run queued` e nunca gera `session:`/`step:` — foi
descartada da fila pelo `run/cancel` antes de executar.

## Evidências

- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — `run started` / `run queued` ×2 / `run failed` / `run started` (#2) + `step:`
- [x] [`logs/start-1.json`](logs/start-1.json), [`logs/start-2.json`](logs/start-2.json), [`logs/start-3.json`](logs/start-3.json) — `state` / `queuePosition`
- [x] [`logs/metrics-during-queue.txt`](logs/metrics-during-queue.txt) — `runs_active 1`, `runs_queued 2`
- [x] [`logs/toolscall-at-capacity.json`](logs/toolscall-at-capacity.json) — `-32000 server at capacity`
- [x] [`logs/cancel-3.json`](logs/cancel-3.json), [`logs/status3-final.json`](logs/status3-final.json) — run #3 `canceled`
- [x] [`logs/status2-1.json`](logs/status2-1.json) … [`logs/status2-final.json`](logs/status2-final.json) — run #2 `queued` → `working`
- [x] [`logs/client.log`](logs/client.log)

## Observações

- Referência de design: item 05 ("No server-level concurrency limit or queue") — implementado.
- `SlowBuild` #1 terminou como `failed` (o passo `Ship` tem `timeout 1s` sobre `sleep 3` e
  auto-termina) — o ponto do cenário é que **o slot libera** e a fila anda, o que aconteceu.
- Um `tools/call` síncrono **não** entra na fila: espera ~5 s por um slot e faz shed
  (`-32000`), diferente do `run/start` que estaciona como `queued`.
- `run_duration_seconds` no `/metrics` exclui o tempo em fila (por isso `count 0` durante a fila:
  nenhuma run havia terminado ainda).
- Servidor encerrado ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** Com `--max-concurrent-runs 1`, apenas uma run executa por vez; as excedentes ficam
`queued` com `queuePosition` crescente; `/metrics` reporta `runs_active`/`runs_queued`
corretos; um `tools/call` com o pool cheio recebe `-32000 server at capacity` após ~5 s;
`run/cancel` numa run `queued` a descarta sem nunca executar; e ao liberar o slot a próxima
da fila passa a `working`.
