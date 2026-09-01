# CENÁRIO-003 — Resultado da Execução (chamada de tool, protocolo MCP stateless)

> Registro de execução gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-003-call.md`](CENARIO-003-call.md) não foi alterado (restrição do cenário).

## Cenário 003: Chamada de uma Tool usando o protocolo MCP stateless

**Objetivo:** Verificar se o cliente pode chamar corretamente uma ferramenta disponível no servidor MCP usando o protocolo stateless.

```gherkin
Dado que o servidor MCP está em execução
E o cliente está autenticado e autorizado a usar a ferramenta
Quando o cliente chama a ferramenta
Então o servidor responde corretamente
```

**Resultado Esperado:** O cliente deve receber uma resposta correta do servidor MCP, indicando que a ferramenta foi chamada com sucesso e fornecendo os resultados esperados da execução da ferramenta.

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 08:59:39 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-003/mhl` (cópia de `src/mhl-runtime/dist/mhl`) |
| Versão | `mhl v1.1.0-alpha-6-g439ea52` |
| Comando do servidor | `mhl serve mcp --http --addr 127.0.0.1:8714 --token <gerado> --state-dir <tmp> tests/cloud` |
| Endpoint | `POST http://127.0.0.1:8714/mcp` |
| Script | [`run.sh`](run.sh) |
| Revisão de protocolo stateless | `2026-07-28` |
| Ferramenta chamada | `DocPipeline` — argumentos `{ repo: "demo-api", approved: "yes" }` |

## Passos executados

1. **Dado que o servidor MCP está em execução** — `healthz` = `200`; startup anuncia `2 tool(s) from .../tests/cloud`.
2. **E o cliente está autenticado e autorizado** — a chamada leva `Authorization: Bearer <token>` (segredo gateway↔mhl). Controle: a mesma `tools/call` **sem** o bearer é rejeitada com `HTTP 401`.
3. **Quando o cliente chama a ferramenta** — um único `POST /mcp` `method: "tools/call"` no modo stateless (sem `initialize`, sem `Mcp-Session-Id`), com `params.name = "DocPipeline"`, `params.arguments` e `params._meta` (protocolo `2026-07-28` + `clientCapabilities`).
4. **Então o servidor responde corretamente** — `HTTP 200`, `result.isError = false`, bloco `content[].text` com o JSON das variáveis finais, `structuredContent` com o mesmo objeto, decorações stateless `resultType: "complete"` e `_meta.serverInfo = mhl/v1.1.0-alpha-6-g439ea52`, e **nenhum** header `Mcp-Session-Id` na resposta.

## Verificações (todas OK)

| Verificação | Esperado | Obtido |
|---|---|---|
| HTTP do `tools/call` stateless | 200 | 200 |
| `result.isError` | `false` | `false` |
| Bloco `content[].type == "text"` com conteúdo | presente | presente |
| `structuredContent.published` | `published docs for demo-api (reviewed)` | idem |
| `structuredContent.review` | `reviewed` | `reviewed` |
| Decoração stateless `resultType` | `complete` | `complete` |
| Decoração stateless `_meta.serverInfo` | `mhl/<versão>` | `mhl/v1.1.0-alpha-6-g439ea52` |
| Header `Mcp-Session-Id` na resposta | ausente (stateless) | ausente |
| **Controle (autz)** `tools/call` sem bearer | `HTTP 401` | `401` |
| **Controle (gate)** `tools/call` com `approved: "no"` | `result.isError = true` | `true` (`step "Review" failed: awaiting documentation review for demo-api`) |

## Resposta do servidor (chamada bem-sucedida)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "_meta": { "io.modelcontextprotocol/serverInfo": { "name": "mhl", "version": "v1.1.0-alpha-6-g439ea52" } },
    "content": [
      { "type": "text", "text": "{\n  \"approved\": \"yes\",\n  \"draft\": \"draft docs for demo-api\\n\",\n  \"published\": \"published docs for demo-api (reviewed)\",\n  \"repo\": \"demo-api\",\n  \"review\": \"reviewed\"\n}" }
    ],
    "isError": false,
    "resultType": "complete",
    "structuredContent": {
      "approved": "yes",
      "draft": "draft docs for demo-api\n",
      "published": "published docs for demo-api (reviewed)",
      "repo": "demo-api",
      "review": "reviewed"
    }
  }
}
```

## Evidências

- [x] Log do servidor MCP: endpoint no ar, `2 tool(s)`, e a execução dos passos da tool
  (`session: …` / `step: Draft` / `step: Review` / `step: Publish` para a chamada OK;
  `step: Draft` / `step: Review` para a chamada barrada no gate) — [`logs/mcp-server.log`](logs/mcp-server.log)
- [x] Headers da resposta stateless (sem `Mcp-Session-Id`) — [`logs/tools-call-headers.txt`](logs/tools-call-headers.txt)
- [x] Resposta da `tools/call` bem-sucedida — [`logs/tools-call-response.json`](logs/tools-call-response.json)
- [x] Controle de autorização: `tools/call` sem bearer (`401`, corpo vazio) — [`logs/tools-call-no-auth-response.json`](logs/tools-call-no-auth-response.json)
- [x] Controle de gate: `tools/call` com `approved: "no"` (`isError: true`) — [`logs/tools-call-review-gate-response.json`](logs/tools-call-review-gate-response.json)
- [x] Trilha completa da execução do cliente — [`logs/client.log`](logs/client.log)

## Observações

- `DocPipeline` executa de forma **síncrona** dentro do `tools/call` (~1–2 s: `Draft` dorme 1 s,
  depois `Review` e `Publish`). Workflows longos/gated devem usar a superfície assíncrona
  `run/start` → `run/status` → `run/resume` (só HTTP), não `tools/call`.
- O `mhl` formata o resultado da tool como o JSON das **variáveis finais** do pipeline, entregue
  duas vezes por compatibilidade: como texto em `content[].text` e como objeto em
  `structuredContent`.
- Uma falha de passo dentro da tool (ex.: o gate de revisão com `approved: "no"`) **não** é um
  erro de transporte: retorna `HTTP 200` com `result.isError = true` e a mensagem do runtime no
  bloco de texto — o servidor "responde corretamente" também nesse caso.
- Sem `--principal-header`, a identidade autorizada é o segredo bearer compartilhado; o
  `context.principal` do workflow fica vazio nesta execução.

## Conclusão

**PASS.** No modo stateless (`2026-07-28`, `params._meta`, sem `initialize`/`Mcp-Session-Id`), o
cliente autenticado chama a ferramenta `DocPipeline` via `tools/call` e recebe a resposta
correta: `isError: false`, o resultado da execução em `content[].text` **e** em
`structuredContent` (`published = "published docs for demo-api (reviewed)"`, `review =
"reviewed"`), com as decorações stateless (`resultType: "complete"`, `_meta.serverInfo`). Os
controles confirmam o contrato: `401` sem o bearer e `isError: true` quando um passo da tool
falha.
