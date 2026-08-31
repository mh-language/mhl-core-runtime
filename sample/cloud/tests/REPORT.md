# Relatório de Execução — Cenários de Teste do Módulo de Cloud (MCP server)

Execução conduzida pelo agente `tester-mcp-server` contra `mhl serve mcp --http`.

| Item | Valor |
|---|---|
| Binário sob teste | `mhl v1.1.0-alpha-6-g439ea52` (cópia de `src/mhl-runtime/dist/mhl`) |
| Workflows servidos | `sample/cloud` → `DocPipeline`, `SlowBuild` |
| Data da execução | 2026-08-31 |
| Total de cenários | 5 |
| Aprovados | 5 |
| Reprovados | 0 |

## Resumo dos cenários

| Código | Descrição | Status | Cenário | Resultado | Logs / Evidências |
|---|---|---|---|---|---|
| CENARIO-001 | Teste de conexão com o servidor MCP — handshake `initialize` sobre Streamable HTTP, sessão emitida e guard de bearer (`401` sem token). | ✅ funcionou | [CENARIO-001-conexao.md](CENARIO-001/CENARIO-001-conexao.md) | [resultado](CENARIO-001/CENARIO-001.resultado.md) | [mcp-server.log](CENARIO-001/logs/mcp-server.log) · [initialize-headers.txt](CENARIO-001/logs/initialize-headers.txt) · [initialize-response.json](CENARIO-001/logs/initialize-response.json) · [client.log](CENARIO-001/logs/client.log) |
| CENARIO-002 | Teste de listagem de tools — `tools/list` após handshake retorna as 2 ferramentas com `name`, `description` e `inputSchema`. | ✅ funcionou | [CENARIO-002-listar-tools.md](CENARIO-002/CENARIO-002-listar-tools.md) | [resultado](CENARIO-002/CENARIO-002-listar-tools.resultado.md) | [mcp-server.log](CENARIO-002/logs/mcp-server.log) · [initialize-headers.txt](CENARIO-002/logs/initialize-headers.txt) · [initialize-response.json](CENARIO-002/logs/initialize-response.json) · [tools-list-response.json](CENARIO-002/logs/tools-list-response.json) · [client.log](CENARIO-002/logs/client.log) |
| CENARIO-002b | Teste de listagem de tools no modo **stateless** (`2026-07-28`, `params._meta`, sem `initialize`/`Mcp-Session-Id`) — lista completa + decorações `resultType: complete` e `_meta.serverInfo`; contratos `-32602` (sem `_meta`) e `-32022` (versão inválida). | ✅ funcionou | [CENARIO-002b-stateless.md](CENARIO-002/CENARIO-002b-stateless.md) | [resultado](CENARIO-002/CENARIO-002b-stateless.resultado.md) | [mcp-server.log](CENARIO-002/logs-stateless/mcp-server.log) · [tools-list-headers.txt](CENARIO-002/logs-stateless/tools-list-headers.txt) · [tools-list-response.json](CENARIO-002/logs-stateless/tools-list-response.json) · [tools-list-no-meta-response.json](CENARIO-002/logs-stateless/tools-list-no-meta-response.json) · [tools-list-bad-version-response.json](CENARIO-002/logs-stateless/tools-list-bad-version-response.json) · [client.log](CENARIO-002/logs-stateless/client.log) |
| CENARIO-003 | Chamada de uma tool no modo **stateless** — `tools/call` autenticado da `DocPipeline` (`repo=demo-api`, `approved=yes`) retorna `isError:false` com o resultado em `content[].text` e `structuredContent`; controles: `401` sem bearer e `isError:true` no gate de revisão (`approved=no`). | ✅ funcionou | [CENARIO-003-call.md](CENARIO-003/CENARIO-003-call.md) | [resultado](CENARIO-003/CENARIO-003-call.resultado.md) | [mcp-server.log](CENARIO-003/logs/mcp-server.log) · [tools-call-headers.txt](CENARIO-003/logs/tools-call-headers.txt) · [tools-call-response.json](CENARIO-003/logs/tools-call-response.json) · [tools-call-no-auth-response.json](CENARIO-003/logs/tools-call-no-auth-response.json) · [tools-call-review-gate-response.json](CENARIO-003/logs/tools-call-review-gate-response.json) · [client.log](CENARIO-003/logs/client.log) |
| CENARIO-003b | Chamada de uma tool **incorreta** no modo stateless — `tools/call` autenticado com `name` inexistente retorna erro JSON-RPC `-32602` `unknown tool "NaoExisteTool"` (HTTP 200, sem `result`, sem execução); controles: nome com caixa errada também `-32602`, nome válido executa normalmente. | ✅ funcionou | [CENARIO-003b-wrong-tool.md](CENARIO-003/CENARIO-003b-wrong-tool.md) | [resultado](CENARIO-003/CENARIO-003b-wrong-tool.resultado.md) | [mcp-server.log](CENARIO-003/logs-wrong-tool/mcp-server.log) · [tools-call-wrong-name-headers.txt](CENARIO-003/logs-wrong-tool/tools-call-wrong-name-headers.txt) · [tools-call-wrong-name-response.json](CENARIO-003/logs-wrong-tool/tools-call-wrong-name-response.json) · [tools-call-wrong-case-response.json](CENARIO-003/logs-wrong-tool/tools-call-wrong-case-response.json) · [tools-call-valid-name-response.json](CENARIO-003/logs-wrong-tool/tools-call-valid-name-response.json) · [client.log](CENARIO-003/logs-wrong-tool/client.log) |

## Cenários planejados — não executados

Escritos, aguardando execução manual. Cada pasta tem o `.md` de especificação e um
`run.sh` autocontido (copia `sample/cloud/mhl` para a própria pasta se não existir;
sobe um servidor numa porta dedicada; grava tudo em `./logs/`; imprime `PASS`/`FAIL`).
Escopo: **um pod só** (sem gateway, sem multi-réplica, sem store compartilhado).

| Código | Descrição | Status | Cenário | Script | Ref. design |
|---|---|---|---|---|---|
| CENARIO-004 | Probes `/healthz` e `/readyz` (200 sem auth); `/metrics` sem auth; `/mcp` 401 sem auth. | ⏳ não executado | [CENARIO-004-probes.md](CENARIO-004/CENARIO-004-probes.md) | [run.sh](CENARIO-004/run.sh) | 06 |
| CENARIO-005 | Bearer no `/mcp` (ausente/errado/correto → 401/401/200); guarda de `Origin` cross-site (403); aviso ao subir sem `--token` em bind não-loopback. | ⏳ não executado | [CENARIO-005-auth-guards.md](CENARIO-005/CENARIO-005-auth-guards.md) | [run.sh](CENARIO-005/run.sh) | 03, 10 |
| CENARIO-006 | Isolamento por principal com `--principal-header`: bob não vê/toca a run de alice (`run/list` vazio, `run/status` → unknown); `--principal-header` sem `--token` recusa iniciar. | ⏳ não executado | [CENARIO-006-principal-isolation.md](CENARIO-006/CENARIO-006-principal-isolation.md) | [run.sh](CENARIO-006/run.sh) | 03 |
| CENARIO-007 | Ciclo assíncrono: `run/start` → poll `run/status` até o gate (`failed`/`resumable`) → `run/resume` → `completed`; `run/list` por dono; outra sessão → unknown; `run/cancel` de SlowBuild `working`. | ⏳ não executado | [CENARIO-007-async-lifecycle.md](CENARIO-007/CENARIO-007-async-lifecycle.md) | [run.sh](CENARIO-007/run.sh) | 01, 04 |
| CENARIO-008 | `--max-concurrent-runs 1`: 3× `run/start` → `working`, `queued` (qp 0/1); `tools/call` com pool cheio → `-32000` "server at capacity"; `run/cancel` de run `queued`; fila anda quando o slot libera. | ⏳ não executado | [CENARIO-008-concurrency-queue.md](CENARIO-008/CENARIO-008-concurrency-queue.md) | [run.sh](CENARIO-008/run.sh) | 05 |
| CENARIO-009 | Drain no `SIGTERM` (`--drain-timeout 20s`): `/readyz` 503 / `/healthz` 200; `run/start` → `-32000` draining; processo espera a run terminar; linha JSON `draining`; checkpoint de shutdown sem `per_step`; `--drain-timeout 0` cancela na hora. | ⏳ não executado | [CENARIO-009-drain-shutdown.md](CENARIO-009/CENARIO-009-drain-shutdown.md) | [run.sh](CENARIO-009/run.sh) | 07 |
| CENARIO-010 | Cap de wall-clock por passo: `SlowBuild.Ship` (`timeout 1s` sobre `sleep 3`) → `failed` em `Ship` com erro de timeout; `run/resume` re-executa `Ship` com orçamento novo. | ⏳ não executado | [CENARIO-010-step-timeout.md](CENARIO-010/CENARIO-010-step-timeout.md) | [run.sh](CENARIO-010/run.sh) | wall-clock cap |
| CENARIO-011 | Estado sobrevive a restart do processo: com `--state-dir`, `run/status`/`run/resume` de um `runId` iniciado por outro processo; sem `--state-dir` → unknown runId após restart. | ⏳ não executado | [CENARIO-011-state-dir-restart.md](CENARIO-011/CENARIO-011-state-dir-restart.md) | [run.sh](CENARIO-011/run.sh) | 01 |
| CENARIO-012 | `/metrics` Prometheus: `runs_total{completed\|canceled}`, `run_duration_seconds_{sum,count}`, `tool_calls_total{ok\|error}`, gauges `runs_active`/`runs_queued`/`sessions_active`; sem auth. | ⏳ não executado | [CENARIO-012-metrics.md](CENARIO-012/CENARIO-012-metrics.md) | [run.sh](CENARIO-012/run.sh) | 08 |
| CENARIO-013 | `run/logs { runId, since? }` cursored e por dono (`step: Draft/Review/Publish`; releitura vazia; não-dono → unknown); stderr com linhas JSON `run started`/`run completed` (runId + owner). | ⏳ não executado | [CENARIO-013-run-logs.md](CENARIO-013/CENARIO-013-run-logs.md) | [run.sh](CENARIO-013/run.sh) | 08, 09 |
| CENARIO-014 | Conformidade de protocolo: método desconhecido (`-32601`), corpo malformado (`-32700`), `MCP-Protocol-Version` inválida (`-32602`), negociação do `initialize`, `DELETE` de sessão (204/404), sessão desconhecida (404), notificação (202), `ping` (legado vs stateless). | ⏳ não executado | [CENARIO-014-protocol-conformance.md](CENARIO-014/CENARIO-014-protocol-conformance.md) | [run.sh](CENARIO-014/run.sh) | — |
| CENARIO-015 | Validação do `inputSchema` anunciado (campo `required` ausente / propriedade extra) em `tools/call` e `run/start`. **Pode expor lacuna** — hoje o `execsvc` só valida os inputs presentes. | ⏳ não executado | [CENARIO-015-inputschema-validation.md](CENARIO-015/CENARIO-015-inputschema-validation.md) | [run.sh](CENARIO-015/run.sh) | — |
| CENARIO-016 | `run/cancel` durante o `Compile` do SlowBuild (dentro de `cmd.exec` com `sleep`) aborta o subprocesso — a run não avança para `Package`. | ⏳ não executado | [CENARIO-016-cancel-inflight.md](CENARIO-016/CENARIO-016-cancel-inflight.md) | [run.sh](CENARIO-016/run.sh) | 01 (direção) |
| CENARIO-K8S-001 | O servidor **dentro de um pod** (Docker Desktop, k8s v1.34.1): probes ligadas ao Service, `SIGTERM` do `kubectl delete pod` drenando como PID 1, `terminationGracePeriodSeconds` × `--drain-timeout`, linhas JSON de ciclo de vida em `kubectl logs`, pod recriado limpo. | ❌ não funcionou — [resultado](CENARIO-K8S-001/CENARIO-K8S-001-pod-lifecycle.resultado.md) · plumbing do pod OK, mas **o `--drain-timeout` não segura a run no SIGTERM** (bug no `internal/mcpserver`) | [CENARIO-K8S-001-pod-lifecycle.md](CENARIO-K8S-001/CENARIO-K8S-001-pod-lifecycle.md) | [run.sh](CENARIO-K8S-001/run.sh) · [drained-pod.log](CENARIO-K8S-001/logs/drained-pod.log) | 06, 07 (k8s glue) |

Portas usadas pelos scripts (host): 004→8721, 005→8722/8723, 006→8724/8725, 007→8726,
008→8727, 009→8728/8729, 010→8730, 011→8731/8732, 012→8733, 013→8734, 014→8735,
015→8736, 016→8737. CENARIO-K8S-001: `port-forward` local em 8791.

### Empacotamento / Kubernetes (um pod)

- `../Dockerfile` (+ `../Dockerfile.dockerignore`) — build multi-stage, cross-compila para a
  arquitetura do daemon (`linux/arm64` em Apple Silicon). **Base `alpine`** (~26 MB), `nonroot`,
  `readOnlyRootFilesystem`. A base começou `distroless`, mas o CENÁRIO-K8S-001 mostrou que os
  workflows precisam de `/bin/sh` (`cmd.exec(["sh","-c",...])`) — daí `alpine`.
  `docker build -f sample/cloud/Dockerfile -t mhl-serve:local .` (a partir da raiz do repo).
- `../k8s/` — Deployment 1 réplica + Service + Secret + probes + `terminationGracePeriodSeconds`,
  estado em `emptyDir` (`deployment.yaml`) ou PVC (`pvc.yaml` + `deployment-durable.yaml`).
  Instruções em [`../k8s/README.md`](../k8s/README.md).
- Alvo `make -C src/mhl-runtime linux-arm64` — binário `linux/arm64` avulso (não entra no `release`).

**Achados do CENÁRIO-K8S-001 (rodado em `docker-desktop`):**
1. *Empacotamento (corrigido):* imagem distroless não roda os workflows — sem `/bin/sh`. Base → `alpine`.
2. *Servidor MCP (aberto):* `--drain-timeout` **não segura a run** no SIGTERM. `internal/mcpserver/http.go:223`
   cria `runsCtx` como filho do contexto de sinal, então o SIGTERM cancela a run por propagação de contexto
   (~1 ms), antes de o dreno esperar. Contradiz o comentário do próprio código e o item 07 do design.
   O CENÁRIO-009 (host) reproduz o mesmo defeito sem Kubernetes.

## Scripts de execução

| Código | Script |
|---|---|
| CENARIO-001 | [CENARIO-001/run.sh](CENARIO-001/run.sh) |
| CENARIO-002 | [CENARIO-002/run.sh](CENARIO-002/run.sh) |
| CENARIO-002b | [CENARIO-002/run-stateless.sh](CENARIO-002/run-stateless.sh) |
| CENARIO-003 | [CENARIO-003/run.sh](CENARIO-003/run.sh) |
| CENARIO-003b | [CENARIO-003/run-wrong-tool.sh](CENARIO-003/run-wrong-tool.sh) |
| CENARIO-004 … 016 | ver a tabela "Cenários planejados" acima |

## Observações

- Nenhum código-fonte do runtime, do módulo de cloud ou do servidor MCP foi alterado.
- Os arquivos `.md` originais dos cenários não foram modificados; cada execução gravou um
  arquivo `*.resultado.md` e uma pasta de logs ao lado do cenário.
- O MCP não define versão **por ferramenta**; `tools/list` traz apenas `name`, `description`
  e `inputSchema`. A versão é do servidor: vem no `initialize` (`serverInfo.version`) e, no
  modo stateless, em `result._meta.io.modelcontextprotocol/serverInfo.version`.
