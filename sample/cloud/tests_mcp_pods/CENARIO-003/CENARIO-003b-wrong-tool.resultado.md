# CENÁRIO-003b — Resultado da Execução (chamada de tool incorreta, protocolo MCP stateless)

> Registro de execução gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-003b-wrong-tool.md`](CENARIO-003b-wrong-tool.md) não foi alterado (restrição do cenário).

## Cenário 003b: Chamada de uma Tool incorreta usando o protocolo MCP stateless

**Objetivo:** Verificar se o cliente pode chamar corretamente uma ferramenta disponível no servidor MCP usando o protocolo stateless.

```gherkin
Dado que o servidor MCP está em execução
E o cliente está autenticado e autorizado a usar a ferramenta
Quando o cliente chama uma ferramenta incorreta
Então o servidor responde com um erro indicando que a ferramenta não foi encontrada
```

**Resultado Esperado:** O cliente deve receber uma resposta de erro do servidor MCP, indicando que a ferramenta chamada não foi encontrada ou não está disponível para uso.

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 09:22:17 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-003/mhl` (cópia de `src/mhl-runtime/dist/mhl`) |
| Versão | `mhl v1.1.0-alpha-6-g439ea52` |
| Comando do servidor | `mhl serve mcp --http --addr 127.0.0.1:8715 --token <gerado> --state-dir <tmp> sample/cloud` |
| Endpoint | `POST http://127.0.0.1:8715/mcp` |
| Script | [`run-wrong-tool.sh`](run-wrong-tool.sh) |
| Revisão de protocolo stateless | `2026-07-28` |
| Nome de tool inexistente chamado | `NaoExisteTool` |

## Passos executados

1. **Dado que o servidor MCP está em execução** — `healthz` = `200`; startup anuncia `2 tool(s)` (`DocPipeline`, `SlowBuild`).
2. **E o cliente está autenticado e autorizado** — todas as chamadas levam `Authorization: Bearer <token>`.
3. **Quando o cliente chama uma ferramenta incorreta** — `POST /mcp` `tools/call` stateless (`params._meta` protocolo `2026-07-28`, sem `initialize`/`Mcp-Session-Id`) com `params.name = "NaoExisteTool"`.
4. **Então o servidor responde com um erro "ferramenta não encontrada"** — `HTTP 200` (o erro é de nível JSON-RPC, no corpo) com `error.code = -32602` e `error.message = unknown tool "NaoExisteTool"`; **nenhum** campo `result`.

## Verificações (todas OK)

| Verificação | Esperado | Obtido |
|---|---|---|
| HTTP do `tools/call` (nome inexistente) | 200 (erro JSON-RPC no corpo) | 200 |
| Corpo tem objeto `error` | sim | sim |
| Corpo **não** tem `result` | sim | sim |
| `error.code` | `-32602` (Invalid params) | `-32602` |
| `error.message` indica tool inexistente | "unknown tool" | `unknown tool "NaoExisteTool"` |
| `error.message` cita o nome chamado | `NaoExisteTool` | presente |
| **Controle (caixa errada)** `name: "docpipeline"` | erro `-32602` | `-32602` |
| **Controle (nome válido)** `name: "DocPipeline"` | sem `error`, `result.isError = false` | confirmado |

## Respostas do servidor

**Tool inexistente** ([`logs-wrong-tool/tools-call-wrong-name-response.json`](logs-wrong-tool/tools-call-wrong-name-response.json)):

```json
{ "jsonrpc": "2.0", "id": 1, "error": { "code": -32602, "message": "unknown tool \"NaoExisteTool\"" } }
```

**Nome válido com caixa errada** ([`logs-wrong-tool/tools-call-wrong-case-response.json`](logs-wrong-tool/tools-call-wrong-case-response.json)) — o match é sensível a maiúsculas/minúsculas:

```json
{ "jsonrpc": "2.0", "id": 2, "error": { "code": -32602, "message": "unknown tool \"docpipeline\"" } }
```

## Evidências

- [x] Log do servidor MCP — [`logs-wrong-tool/mcp-server.log`](logs-wrong-tool/mcp-server.log).
  Nota: só aparece **um** bloco `session:` / `step: Draft/Review/Publish` — o do controle com
  nome válido. As duas chamadas com nome incorreto **não geram nenhum `step:`**: são rejeitadas
  no dispatch, antes de o interpretador iniciar ou de um diretório de run ser criado.
- [x] Headers da resposta de erro (sem `Mcp-Session-Id`) — [`logs-wrong-tool/tools-call-wrong-name-headers.txt`](logs-wrong-tool/tools-call-wrong-name-headers.txt)
- [x] Resposta de erro para tool inexistente — [`logs-wrong-tool/tools-call-wrong-name-response.json`](logs-wrong-tool/tools-call-wrong-name-response.json)
- [x] Resposta de erro para nome com caixa errada — [`logs-wrong-tool/tools-call-wrong-case-response.json`](logs-wrong-tool/tools-call-wrong-case-response.json)
- [x] Controle com nome válido (executa normalmente) — [`logs-wrong-tool/tools-call-valid-name-response.json`](logs-wrong-tool/tools-call-valid-name-response.json)
- [x] Trilha completa da execução do cliente — [`logs-wrong-tool/client.log`](logs-wrong-tool/client.log)

## Observações

- O erro para tool inexistente é de **nível JSON-RPC** (`error` no envelope, código `-32602`),
  distinto de uma falha *dentro* de uma tool válida (que retorna `HTTP 200` +
  `result.isError = true`, como no CENÁRIO-003). O transporte HTTP continua `200` nos dois casos.
- A resolução de nome de tool é **case-sensitive**: `docpipeline` ≠ `DocPipeline`.
- Como a rejeição acontece antes de qualquer execução, uma tool incorreta não consome slot de
  concorrência nem cria diretório temporário de run.

## Conclusão

**PASS.** Ao chamar uma ferramenta incorreta no modo stateless, o servidor MCP responde com erro
JSON-RPC `-32602` e mensagem `unknown tool "<nome>"`, sem campo `result` e sem executar nada. Os
controles confirmam: nome com caixa errada também é "não encontrada" (`-32602`), e o nome válido
correspondente executa normalmente — ou seja, é o **nome** que é rejeitado, não o transporte nem
a autenticação.
