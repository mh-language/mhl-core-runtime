# CENÁRIO-002 — Resultado da Execução

> Registro de execução gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-002-listar-tools.md`](CENARIO-002-listar-tools.md) não foi alterado (restrição do cenário).

## Cenário 002: Teste de Listagem de Tools

**Objetivo:** Verificar se o cliente pode listar corretamente as ferramentas disponíveis no servidor MCP.

```gherkin
Dado que o servidor MCP está em execução
Quando o cliente solicita a listagem de ferramentas disponíveis
Então o servidor MCP deve retornar a lista completa de ferramentas
```

**Resultado Esperado:** O cliente deve receber uma lista completa de ferramentas disponíveis no servidor MCP, incluindo informações como nome, versão e descrição de cada ferramenta.

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 08:25:32 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-002/mhl` (cópia de `src/mhl-runtime/dist/mhl`) |
| Versão | `mhl v1.1.0-alpha-6-g439ea52` |
| Comando do servidor | `mhl serve mcp --http --addr 127.0.0.1:8712 --token <gerado> --state-dir <tmp> tests/cloud` |
| Endpoint | `POST http://127.0.0.1:8712/mcp` |
| Script | [`run.sh`](run.sh) |

## Passos executados

1. **Dado que o servidor MCP está em execução** — script sobe `mhl serve mcp --http` e aguarda `GET /healthz` = `200`. Startup anuncia `2 tool(s) from .../tests/cloud`.
2. **Handshake** — `POST /mcp` `initialize` (`HTTP 200`, sessão `727d3ec406438233549fde3f8e84e6e0`).
3. **Quando o cliente solicita a listagem** — `POST /mcp` `method: "tools/list"` com a sessão.
4. **Então o servidor retorna a lista completa** — `HTTP 200`, `result.tools` com **2** entradas.

## Verificações (todas OK)

| Verificação | Esperado | Obtido |
|---|---|---|
| HTTP do `initialize` | 200 | 200 |
| HTTP do `tools/list` | 200 | 200 |
| Campo `result.tools` | presente | presente |
| Qtde de tools | ≥ 1 e = anúncio de startup (2) | 2 |
| Tool `DocPipeline` presente | sim | sim |
| Tool `SlowBuild` presente | sim | sim |
| Todas com `description` | 2/2 | 2/2 |
| Todas com `inputSchema` (JSON Schema) | 2/2 | 2/2 |

## Lista de ferramentas retornada

| name | description | inputSchema (campos obrigatórios) |
|---|---|---|
| `DocPipeline` | Draft docs for a repo, wait for human review, then publish. | `repo: string`, `approved: string` |
| `SlowBuild` | Three ~3s stages: compile, package, ship. | `target: string` |

Resposta bruta em [`logs/tools-list-response.json`](logs/tools-list-response.json).

## Observação sobre "versão de cada ferramenta"

O protocolo MCP (`tools/list`) define por ferramenta apenas `name`, `description`,
`inputSchema` (e `annotations`/`outputSchema` opcionais) — **não há campo de versão por
ferramenta**. A versão é um atributo do servidor, entregue no `initialize` em
`result.serverInfo.version` = `v1.1.0-alpha-6-g439ea52` (ver
[`logs/initialize-response.json`](logs/initialize-response.json)). Nome e descrição de cada
ferramenta foram retornados conforme esperado.

## Evidências

- [x] Log do servidor MCP mostrando o endpoint no ar e a contagem de tools — [`logs/mcp-server.log`](logs/mcp-server.log)
- [x] Headers do `initialize` com `Mcp-Session-Id` — [`logs/initialize-headers.txt`](logs/initialize-headers.txt)
- [x] Resposta do `initialize` (traz `serverInfo.version`) — [`logs/initialize-response.json`](logs/initialize-response.json)
- [x] Resposta do `tools/list` (lista completa) — [`logs/tools-list-response.json`](logs/tools-list-response.json)
- [x] Trilha completa da execução do cliente — [`logs/client.log`](logs/client.log)

## Conclusão

**PASS.** O cliente lista as ferramentas do servidor MCP via `tools/list`: o servidor retorna
as **2** ferramentas (`DocPipeline`, `SlowBuild`) — a mesma contagem anunciada no startup —,
cada uma com `name`, `description` e `inputSchema` completo. Versão por ferramenta não existe
no MCP; a versão do servidor (`v1.1.0-alpha-6-g439ea52`) vem no `initialize`.
