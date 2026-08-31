# CENÁRIO-013 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-013-run-logs.md`](CENARIO-013-run-logs.md) não foi alterado.

## Cenário 013: Logs por run (`run/logs`) e eventos de ciclo de vida estruturados

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 13:43 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-013/mhl` (cópia de `sample/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8734 --token <gerado> --state-dir <tmp> sample/cloud` |
| Modo | handshake — sessão A (dona), sessão B (não-dona) |
| Script | [`run.sh`](run.sh) |

## Passos e resultados

| Passo | Esperado | Obtido |
|---|---|---|
| `run/start DocPipeline {approved:"yes"}` → poll | `completed` | `runId=38990b24…`, `working` → `completed` |
| `run/logs { runId }` (sessão A) | `{runId, text, nextSince}` com `step: Draft/Review/Publish`; `nextSince` int > 0 | `text = "session: 38990b24…\nstep: Draft\nstep: Review\nstep: Publish\n"`, `nextSince = 81` |
| `run/logs { runId, since: 81 }` (sessão A) | `text` vazio, `nextSince` igual, sem `error` | `text = ""`, `nextSince = 81`, sem `error` |
| `run/logs { runId }` (sessão B) | `-32602 unknown runId` | `{"error":{"code":-32602,"message":"unknown runId \"38990b24…\""}}` |
| stderr: `"msg":"run started"` e `"msg":"run completed"` com `runId` + `owner` | ambos presentes | ✔ (ver abaixo) |

## Eventos de ciclo de vida no stderr ([`logs/lifecycle-events.jsonl`](logs/lifecycle-events.jsonl))

```json
{"...","msg":"run started","runId":"38990b24…","owner":"36e48f22…","tool":"DocPipeline","resume":false}
{"...","msg":"run completed","runId":"38990b24…","owner":"36e48f22…","tool":"DocPipeline","durationMs":1016,"steps":3}
```

## Evidências

- [x] [`logs/run-logs-1.json`](logs/run-logs-1.json) — 1ª leitura (`text` com os `step:`, `nextSince:81`)
- [x] [`logs/run-logs-2.json`](logs/run-logs-2.json) — releitura com `since:81` (`text:""`, `nextSince:81`)
- [x] [`logs/run-logs-nonowner.json`](logs/run-logs-nonowner.json) — sessão B → `-32602 unknown runId`
- [x] [`logs/lifecycle-events.jsonl`](logs/lifecycle-events.jsonl) — `run started` / `run completed` com `runId` + `owner`
- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — stderr completo (`step:` de cada estágio + eventos JSON)
- [x] [`logs/client.log`](logs/client.log)

## Observações

- Referência de design: itens 08 e 09.
- O mesmo `text` vai para o stderr do processo (`kubectl logs` / CloudWatch); `run/logs` é a
  cópia por run num ring de 64 KiB. O `dropped:true` (cursor na região descartada) não foi
  exercitado — a saída de `DocPipeline` (81 bytes) cabe folgado no ring.
- A releitura a partir de `nextSince` retornou `text` vazio e o mesmo cursor porque não houve
  saída nova depois da 1ª leitura (a run já estava `completed`).
- `run/logs` é owner-scoped como `run/status`: o não-dono recebe o mesmo `-32602` de um
  `runId` inexistente.
- Servidor encerrado ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** `run/logs { runId }` devolve `{runId, text, nextSince}` com a saída `step:` da run
e um cursor de bytes; a releitura com `since` retorna vazio e cursor estável, sem erro;
`run/logs` de outra sessão é negado com `-32602 unknown runId`; e o stderr do processo
carrega as linhas JSON `run started` / `run completed` com `runId` e `owner`.
