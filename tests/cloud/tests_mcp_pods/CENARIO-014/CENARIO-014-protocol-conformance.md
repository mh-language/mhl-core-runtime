# Cenário 014: Conformidade de protocolo JSON-RPC / MCP

**Objetivo:** Verificar as bordas do endpoint de protocolo do pod: método
desconhecido, corpo malformado, versão de protocolo não suportada, negociação do
`initialize`, encerramento de sessão via `DELETE`, sessão desconhecida,
notificação sem `id` e `ping`.

```gherkin
Dado que o servidor MCP está em execução
Quando o cliente envia requisições fora do contrato
Então o servidor responde com os códigos JSON-RPC / HTTP corretos
```

**Resultado Esperado:**

| Requisição | Esperado |
|---|---|
| método `foo/bar` (com `id`, sessão válida) | `-32601` `method not found: foo/bar` |
| corpo não-JSON | HTTP `400`, `-32700` parse error |
| header `MCP-Protocol-Version: 9999-99-99` | HTTP `400`, `-32602` `unsupported MCP-Protocol-Version: 9999-99-99` |
| `initialize` com `protocolVersion: "1999-01-01"` | HTTP `200`, `result.protocolVersion == "2025-06-18"` (negocia para baixo, **sem** erro) |
| `DELETE /mcp` com `Mcp-Session-Id` válido | HTTP `204` |
| `DELETE /mcp` com `Mcp-Session-Id` inexistente | HTTP `404` |
| `POST /mcp` com `Mcp-Session-Id` inexistente | HTTP `404` |
| notificação (sem `id`, ex. `notifications/initialized`) | HTTP `202`, corpo vazio |
| `ping` em sessão legada (`initialize` feito) | `result: {}` |
| `ping` em modo stateless (`params._meta`) | `-32601` `method not found: ping` |

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Matriz de requisição → HTTP/JSON-RPC observado
- [ ] Corpos de resposta de cada caso

### Observações:
- `initialize` com versão desconhecida **não** é erro — o servidor negocia para
  `2025-06-18` (`defaultHandshakeVersion`). O `-32022` só ocorre no modo stateless
  (`params._meta` com versão não suportada) — coberto no CENÁRIO-002b.
- `ping` foi removido em `2026-07-28`; só é honrado em conexão legada.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
