# Regressivo — tests_mcp_pods

**Execução:** 20260901-221022 · binário `mhl v1.2.1-beta.1-dirty` · TIMEOUT 240s · RUN_K8S=0

| Código | Variante | Status | Tempo | Log |
|---|---|---|---|---|
| 001 | conexao | PASS | 1s | [logs-regression/CENARIO-001-conexao.log](logs-regression/CENARIO-001-conexao.log) |
| 002 | listar-tools | PASS | 0s | [logs-regression/CENARIO-002-listar-tools.log](logs-regression/CENARIO-002-listar-tools.log) |
| 002 | stateless | PASS | 1s | [logs-regression/CENARIO-002-stateless.log](logs-regression/CENARIO-002-stateless.log) |
| 003 | call | PASS | 3s | [logs-regression/CENARIO-003-call.log](logs-regression/CENARIO-003-call.log) |
| 003 | wrong-tool | PASS | 2s | [logs-regression/CENARIO-003-wrong-tool.log](logs-regression/CENARIO-003-wrong-tool.log) |
| 004 | probes | PASS | 0s | [logs-regression/CENARIO-004-probes.log](logs-regression/CENARIO-004-probes.log) |
| 005 | auth-guards | PASS | 2s | [logs-regression/CENARIO-005-auth-guards.log](logs-regression/CENARIO-005-auth-guards.log) |
| 006 | principal-isolation | PASS | 4s | [logs-regression/CENARIO-006-principal-isolation.log](logs-regression/CENARIO-006-principal-isolation.log) |
| 007 | async-lifecycle | PASS | 4s | [logs-regression/CENARIO-007-async-lifecycle.log](logs-regression/CENARIO-007-async-lifecycle.log) |
| 008 | concurrency-queue | PASS | 8s | [logs-regression/CENARIO-008-concurrency-queue.log](logs-regression/CENARIO-008-concurrency-queue.log) |
| 009 | drain-shutdown | PASS | 9s | [logs-regression/CENARIO-009-drain-shutdown.log](logs-regression/CENARIO-009-drain-shutdown.log) |
| 010 | step-timeout | PASS | 9s | [logs-regression/CENARIO-010-step-timeout.log](logs-regression/CENARIO-010-step-timeout.log) |
| 011 | state-dir-restart | PASS | 5s | [logs-regression/CENARIO-011-state-dir-restart.log](logs-regression/CENARIO-011-state-dir-restart.log) |
| 012 | metrics | PASS | 4s | [logs-regression/CENARIO-012-metrics.log](logs-regression/CENARIO-012-metrics.log) |
| 013 | run-logs | PASS | 2s | [logs-regression/CENARIO-013-run-logs.log](logs-regression/CENARIO-013-run-logs.log) |
| 014 | protocol-conformance | PASS | 1s | [logs-regression/CENARIO-014-protocol-conformance.log](logs-regression/CENARIO-014-protocol-conformance.log) |
| 015 | inputschema-validation | PASS | 1s | [logs-regression/CENARIO-015-inputschema-validation.log](logs-regression/CENARIO-015-inputschema-validation.log) |
| 016 | cancel-inflight | PASS | 5s | [logs-regression/CENARIO-016-cancel-inflight.log](logs-regression/CENARIO-016-cancel-inflight.log) |
| K8S-001 | pod-lifecycle | SKIP | — | não incluído (RUN_K8S!=1) |

**Resumo:** 18 PASS · 0 FAIL · 1 SKIP de 18 executados.
