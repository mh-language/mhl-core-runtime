# CENÁRIO-014 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-014-protocol-conformance.md`](CENARIO-014-protocol-conformance.md) não foi alterado.

## Cenário 014: Conformidade de protocolo JSON-RPC / MCP

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 13:46 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-014/mhl` (cópia de `tests/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8735 --token <gerado> --state-dir <tmp> tests/cloud` |
| Script | [`run.sh`](run.sh) |

## Matriz observada ([`logs/matrix.txt`](logs/matrix.txt))

| Requisição | Esperado | Obtido |
|---|---|---|
| método `foo/bar` (id, sessão) | `-32601` `method not found: foo/bar` | HTTP 200 · `-32601` · `method not found: foo/bar` |
| corpo não-JSON | HTTP `400`, `-32700` | HTTP 400 · `-32700` · `parse error: invalid character 'i' ...` |
| header `MCP-Protocol-Version: 9999-99-99` | HTTP `400`, `-32602` `unsupported MCP-Protocol-Version: 9999-99-99` | HTTP 400 · `-32602` · `unsupported MCP-Protocol-Version: 9999-99-99` |
| `initialize` `protocolVersion:"1999-01-01"` | HTTP `200`, `result.protocolVersion == "2025-06-18"` (sem erro) | HTTP 200 · `result.protocolVersion = "2025-06-18"` |
| `DELETE /mcp` `Mcp-Session-Id` válido | HTTP `204` | HTTP 204 |
| `DELETE /mcp` `Mcp-Session-Id` inexistente | HTTP `404` | HTTP 404 |
| `POST /mcp` `Mcp-Session-Id` inexistente | HTTP `404` | HTTP 404 |
| notificação `notifications/initialized` (sem `id`) | HTTP `202`, corpo vazio | HTTP 202 · 0 bytes |
| `ping` em sessão legada | `result: {}` | HTTP 200 · `{"result":{}}` |
| `ping` em modo stateless (`params._meta`) | `-32601` `method not found: ping` | `-32601` · `method not found: ping` |

## Corpos de resposta (amostra)

```json
// foo/bar
{"jsonrpc":"2.0","id":10,"error":{"code":-32601,"message":"method not found: foo/bar"}}
// corpo não-JSON
{"jsonrpc":"2.0","error":{"code":-32700,"message":"parse error: invalid character 'i' looking for beginning of value"}}
// header de versão inválida
{"jsonrpc":"2.0","id":11,"error":{"code":-32602,"message":"unsupported MCP-Protocol-Version: 9999-99-99"}}
// initialize com versão bogus -> negocia (sem erro)
{"jsonrpc":"2.0","id":12,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"mhl",...}}}
// ping legado
{"jsonrpc":"2.0","id":14,"result":{}}
// ping stateless
{"jsonrpc":"2.0","id":15,"error":{"code":-32601,"message":"method not found: ping"}}
```

## Evidências

- [x] [`logs/matrix.txt`](logs/matrix.txt) — requisição → HTTP/JSON-RPC observado
- [x] Corpos: [`unknown-method.json`](logs/unknown-method.json), [`malformed-json.json`](logs/malformed-json.json), [`bad-proto-header.json`](logs/bad-proto-header.json), [`init-bogus-version.json`](logs/init-bogus-version.json), [`ping-legacy.json`](logs/ping-legacy.json), [`ping-stateless.json`](logs/ping-stateless.json), [`notif-body.txt`](logs/notif-body.txt) (vazio)
- [x] [`logs/mcp-server.log`](logs/mcp-server.log), [`logs/client.log`](logs/client.log)

## Observações

- Erros de **nível JSON-RPC** (método desconhecido, `ping` stateless) voltam com HTTP `200` e
  `error` no corpo; erros de **transporte** (corpo malformado, header de versão inválida) voltam
  com HTTP `400`. Sessão desconhecida (`POST`/`DELETE`) é HTTP `404` puro (sem corpo JSON-RPC).
- `initialize` com versão desconhecida **não** é erro — negocia para `2025-06-18`
  (`defaultHandshakeVersion`). O `-32022` só ocorre no modo stateless (CENÁRIO-002b).
- Após o `DELETE /mcp` a sessão `87ce416a…` deixou de existir (204); outra sessão foi usada
  para os demais passos.
- Servidor encerrado ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** Todas as 10 bordas de protocolo respondem conforme o contrato: método desconhecido
`-32601`, corpo malformado `400/-32700`, `MCP-Protocol-Version` não suportada `400/-32602`,
`initialize` negocia versão desconhecida para `2025-06-18` sem erro, `DELETE` de sessão
`204`/`404`, sessão desconhecida `404`, notificação `202` sem corpo, `ping` legado `result:{}`
e `ping` stateless `-32601`.
