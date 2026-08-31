# CENÁRIO-001 — Resultado da Execução

> Registro de execução gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-001.md`](CENARIO-001.md) não foi alterado (restrição do cenário).

## Cenário 1: Teste de Conexão com o Servidor MCP

**Objetivo:** Verificar se um cliente pode se conectar com sucesso ao servidor MCP.

```gherkin
Dado que o servidor MCP está em execução
Quando o cliente tenta se conectar ao servidor MCP
Então a conexão deve ser estabelecida com sucesso
```

**Resultado Esperado:** O cliente deve receber uma confirmação de conexão bem-sucedida do servidor MCP.

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 08:09:24 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-001/mhl` (cópia de `src/mhl-runtime/dist/mhl`) |
| Versão | `mhl v1.1.0-alpha-6-g439ea52` |
| Comando do servidor | `mhl serve mcp --http --addr 127.0.0.1:8711 --token <gerado> --state-dir <tmp> sample/cloud` |
| Endpoint | `POST http://127.0.0.1:8711/mcp` |
| Workflows carregados | 2 tools (`DocPipeline`, `SlowBuild`) de `sample/cloud` |
| Script | [`run.sh`](run.sh) |

## Passos executados

1. **Dado que o servidor MCP está em execução** — o script sobe `mhl serve mcp --http` em background e aguarda o probe `GET /healthz` retornar `200` (pronto na 2ª tentativa, ~0,4 s).
2. **Quando o cliente tenta se conectar** — `POST /mcp` com `method: "initialize"` (`protocolVersion 2025-06-18`), portando o bearer compartilhado `Authorization: Bearer <token>`.
3. **Então a conexão deve ser estabelecida com sucesso** — resposta `HTTP 200` com envelope JSON-RPC `result`, `serverInfo` e um header `Mcp-Session-Id` emitido.

## Verificações (todas OK)

| Verificação | Esperado | Obtido |
|---|---|---|
| Código HTTP do `initialize` | `200` | `200` |
| Campo `result` no corpo JSON-RPC | presente | presente |
| `result.protocolVersion` | presente | `2025-06-18` |
| `result.serverInfo` | presente | `{"name":"mhl","version":"v1.1.0-alpha-6-g439ea52"}` |
| Header `Mcp-Session-Id` | emitido | `9b57683e46d288e8d2aa518217b0007f` |
| Controle negativo: `initialize` sem bearer | `401` | `401` |

## Resposta do servidor ao `initialize`

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "capabilities": { "tools": { "listChanged": false } },
    "protocolVersion": "2025-06-18",
    "serverInfo": { "name": "mhl", "version": "v1.1.0-alpha-6-g439ea52" }
  }
}
```

## Evidências

- [x] Log do servidor MCP mostrando o endpoint no ar e as tools carregadas — [`logs/mcp-server.log`](logs/mcp-server.log)
  (`mhl serve mcp --http: 2 tool(s) from .../sample/cloud on http://127.0.0.1:8711/mcp`)
- [x] Headers da resposta, com `Mcp-Session-Id` (sessão do cliente estabelecida) — [`logs/initialize-headers.txt`](logs/initialize-headers.txt)
- [x] Corpo JSON-RPC da resposta de `initialize` — [`logs/initialize-response.json`](logs/initialize-response.json)
- [x] Trilha completa da execução do cliente — [`logs/client.log`](logs/client.log)

## Conclusão

**PASS.** Um cliente consegue se conectar ao servidor MCP (`mhl serve mcp --http`) via handshake
`initialize` sobre Streamable HTTP: o servidor responde `200` com `serverInfo`/`protocolVersion` e
abre uma sessão (`Mcp-Session-Id`). O guard de bearer também está ativo — sem token o mesmo
`initialize` é rejeitado com `401`.
