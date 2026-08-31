# CENÁRIO-006 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-006-principal-isolation.md`](CENARIO-006-principal-isolation.md) não foi alterado.

## Cenário 006: Isolamento de runs por principal

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 12:44 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-006/mhl` (cópia de `sample/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8724 --token <gerado> --principal-header X-Mhl-Principal --state-dir <tmp> sample/cloud` |
| Modo | stateless (`params._meta`), com `X-Mhl-Principal` em cada request |
| Principais | `alice@acme.com`, `bob@acme.com` |
| Script | [`run.sh`](run.sh) |

## Passos e resultados

| Passo | Esperado | Obtido |
|---|---|---|
| `alice` `run/start DocPipeline {repo:"iso-demo", approved:"no"}` | `runId` | `runId=5834873f482f7a3e4b73692421bf393a` (HTTP 200) |
| `alice` `run/status` (após ~2 s) | run parada no gate, `resumable` | `state:"failed"`, `step:"Review"`, `reached:["Draft","Review"]`, `resumable:true` |
| `bob` `run/list` | `{"runs":[]}` | `{"runs":[]}` |
| `bob` `run/status {runId de alice}` | `-32602` `unknown runId` | `{"error":{"code":-32602,"message":"unknown runId \"5834873f...\""}}` |
| `alice` `run/list` | contém o `runId` | 1 run, `runId=5834873f...`, `tool:"DocPipeline"`, `state:"failed"` |
| Servidor com `--principal-header` **sem** `--token` | sai com erro explicando a exigência | processo encerrou; stderr: `mhl: --principal-header needs --token / MHL_SERVE_TOKEN: without the shared gateway↔mhl secret the header is client-spoofable` |

## Evidência do ownership por identidade

No log do servidor ([`logs/mcp-server.log`](logs/mcp-server.log)), a run de `alice` foi
registrada com:

```
"msg":"run started","runId":"5834873f...","owner":"682471df80896a3870b5f888d2fd4cf439f9c598a3cd7905dfb33d949357bdca","tool":"DocPipeline"
```

O `owner` é o hash da **identidade verificada** (`ownerFor("alice@acme.com")`), não um
hash de sessão — é isso que faz `run/list` / `run/status` de `bob` não enxergarem a run,
e um não-dono receber o mesmo `-32602` de um `runId` inexistente (o método não é um
oráculo de existência).

## Evidências

- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — `run started`/`run failed` com `owner` = hash do principal
- [x] [`logs/alice-run-start.json`](logs/alice-run-start.json), [`logs/alice-run-status.json`](logs/alice-run-status.json), [`logs/alice-run-list.json`](logs/alice-run-list.json)
- [x] [`logs/bob-run-list.json`](logs/bob-run-list.json) (`{"runs":[]}`), [`logs/bob-run-status.json`](logs/bob-run-status.json) (`-32602`)
- [x] [`logs/start-without-token.log`](logs/start-without-token.log) — recusa de `--principal-header` sem `--token`
- [x] [`logs/client.log`](logs/client.log) — trilha completa

## Observações

- Simula a identidade que o API Gateway injetaria: header confiável (`X-Mhl-Principal`) +
  segredo compartilhado (`--token`). Sem o segredo, o header seria forjável pelo cliente —
  por isso o servidor recusa `--principal-header` sozinho.
- `context.principal` chegando ao workflow não foi verificado (exige um bloco `context:`
  no `.mh`, que os workflows de `sample/cloud` não têm).
- Servidor(es) encerrado(s) ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** Com `--principal-header X-Mhl-Principal` + `--token`, o pod isola runs por
identidade verificada: `bob` não vê (`run/list` vazio) nem acessa (`run/status` →
`-32602 unknown runId`) a run de `alice`, enquanto `alice` vê a própria em `run/list`.
`--principal-header` sem `--token` é recusado na largada.
