# CENÁRIO-012 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-012-metrics.md`](CENARIO-012-metrics.md) não foi alterado.

## Cenário 012: Métricas Prometheus em `/metrics`

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 13:35 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-012/mhl` (cópia de `tests/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8733 --token <gerado> --state-dir <tmp> tests/cloud` |
| Script | [`run.sh`](run.sh) |

## Atividade gerada

| Ação | Resultado |
|---|---|
| `run/start DocPipeline {approved:"yes"}` → poll | `completed` (`durationMs:1013`) |
| `run/start SlowBuild` + `run/cancel` → poll | `canceled` (`durationMs:1051`) |
| `tools/call DocPipeline {approved:"yes"}` | `isError:false` (ok) |
| `tools/call {name:"NaoExiste"}` | `error.code:-32602` (error) |

## `/metrics` — antes vs. depois

| Série | antes ([`metrics-before.txt`](logs/metrics-before.txt)) | depois ([`metrics-after.txt`](logs/metrics-after.txt)) | Esperado |
|---|---|---|---|
| `mhl_serve_runs_total{outcome="completed"}` | 0 | **1** | ≥ 1 ✔ |
| `mhl_serve_runs_total{outcome="canceled"}` | 0 | **1** | ≥ 1 ✔ |
| `mhl_serve_runs_total{outcome="failed"}` | 0 | 0 | — |
| `mhl_serve_run_duration_seconds_count` | 0 | **2** | ≥ 2 ✔ |
| `mhl_serve_run_duration_seconds_sum` | 0.000 | **2.064** | — (≈ 1.013 + 1.051) |
| `mhl_serve_tool_calls_total{outcome="ok"}` | 0 | **1** | ≥ 1 ✔ |
| `mhl_serve_tool_calls_total{outcome="error"}` | 0 | **1** | ≥ 1 ✔ |
| `mhl_serve_runs_active` (gauge) | 0 | 0 | presente ✔ |
| `mhl_serve_runs_queued` (gauge) | 0 | 0 | presente ✔ |
| `mhl_serve_sessions_active` (gauge) | 0 | 1 | presente ✔ |

- `GET /metrics` **sem bearer** → HTTP `200`, `Content-Type: text/plain; version=0.0.4; charset=utf-8`.
- `run_duration_seconds` conta **completed + canceled** (2); os `tools/call` vão para
  `tool_calls_total`, não entram no duration.

## Evidências

- [x] [`logs/metrics-before.txt`](logs/metrics-before.txt) / [`logs/metrics-after.txt`](logs/metrics-after.txt) — corpo Prometheus completo
- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — `run completed` (DocPipeline, 1013 ms) e `run canceled` (SlowBuild, 1051 ms)
- [x] [`logs/run-completed-*.json`](logs/), [`logs/run-canceled-*.json`](logs/) — as runs que geraram os contadores
- [x] [`logs/toolscall-ok.json`](logs/toolscall-ok.json) (`isError:false`), [`logs/toolscall-err.json`](logs/toolscall-err.json) (`-32602`)
- [x] [`logs/client.log`](logs/client.log)

## Observações

- Referência de design: item 08 ("Unstructured diagnostics, no metrics") — implementado como
  texto Prometheus in-process (`promMetrics`).
- `/metrics` é `404` se o sink for push-based (OTLP/extensão); aqui é o `promMetrics` embutido.
- Servidor encerrado ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** `GET /metrics` responde `200` sem autenticação, com `Content-Type` Prometheus, e
os contadores/gauges refletem a atividade: `runs_total{completed}=1`, `{canceled}=1`,
`run_duration_seconds_count=2` (sum ≈ 2.064), `tool_calls_total{ok}=1` e `{error}=1`, e as
gauges `runs_active` / `runs_queued` / `sessions_active` presentes.
