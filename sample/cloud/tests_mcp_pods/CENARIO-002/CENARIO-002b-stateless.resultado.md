# CENÁRIO-002b — Resultado da Execução (protocolo MCP stateless)

> Registro de execução gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-002b-stateless.md`](CENARIO-002b-stateless.md) não foi alterado (restrição do cenário).

## Cenário 002b: Teste de Listagem de Tools usando o protocolo MCP stateless

**Objetivo:** Verificar se o cliente pode listar corretamente as ferramentas disponíveis no servidor MCP.

```gherkin
Dado que o servidor MCP está em execução
Quando o cliente solicita a listagem de ferramentas disponíveis
E o protocolo informado é stateless
Então o servidor MCP deve retornar a lista completa de ferramentas
```

**Resultado Esperado:** O cliente deve receber uma lista completa de ferramentas disponíveis no servidor MCP, incluindo informações como nome, versão e descrição de cada ferramenta.

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 08:36:25 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-002/mhl` (cópia de `src/mhl-runtime/dist/mhl`) |
| Versão | `mhl v1.1.0-alpha-6-g439ea52` |
| Comando do servidor | `mhl serve mcp --http --addr 127.0.0.1:8713 --token <gerado> --state-dir <tmp> sample/cloud` |
| Endpoint | `POST http://127.0.0.1:8713/mcp` |
| Script | [`run-stateless.sh`](run-stateless.sh) |
| Revisão de protocolo stateless | `2026-07-28` |

## O que "stateless" significa aqui

Modo stateless (revisão `2026-07-28`, "Per-request protocol fields"): **sem** `initialize`,
**sem** header `Mcp-Session-Id`. Cada request é auto-contido e carrega em `params._meta`:

- `io.modelcontextprotocol/protocolVersion` = `"2026-07-28"`
- `io.modelcontextprotocol/clientCapabilities` = `{}`

O servidor responde sem abrir sessão e decora o `result` com `resultType: "complete"` e
`_meta.io.modelcontextprotocol/serverInfo`.

## Passos executados

1. **Dado que o servidor MCP está em execução** — `healthz` = `200`; startup anuncia `2 tool(s) from .../sample/cloud`.
2. **Quando o cliente solicita a listagem / E o protocolo informado é stateless** — um único `POST /mcp` `tools/list`, sem `initialize` e sem `Mcp-Session-Id`, com `params._meta` (protocolo `2026-07-28`) e header `MCP-Protocol-Version: 2026-07-28`.
3. **Então o servidor retorna a lista completa** — `HTTP 200`, `result.tools` com **2** entradas, `resultType: "complete"`, `_meta.serverInfo = mhl/v1.1.0-alpha-6-g439ea52`, e **nenhum** header `Mcp-Session-Id` na resposta.

## Verificações (todas OK)

| Verificação | Esperado | Obtido |
|---|---|---|
| HTTP do `tools/list` stateless | 200 | 200 |
| `result.tools` presente | sim | sim |
| Qtde de tools = anúncio de startup | 2 | 2 |
| `DocPipeline` / `SlowBuild` presentes | sim | sim |
| Todas com `description` | 2/2 | 2/2 |
| Todas com `inputSchema` | 2/2 | 2/2 |
| Decoração stateless `resultType` | `complete` | `complete` |
| Decoração stateless `_meta.serverInfo` | `mhl/<versão>` | `mhl/v1.1.0-alpha-6-g439ea52` |
| Header `Mcp-Session-Id` na resposta | ausente (stateless) | ausente |
| **Controle (a)** `tools/list` sem `_meta` | erro `-32602` | `-32602` |
| **Controle (b)** `tools/list` com `protocolVersion` inválida | erro `-32022` | `-32022` |

## Lista de ferramentas retornada (stateless)

| name | description | inputSchema (obrigatórios) |
|---|---|---|
| `DocPipeline` | Draft docs for a repo, wait for human review, then publish. | `repo: string`, `approved: string` |
| `SlowBuild` | Three ~3s stages: compile, package, ship. | `target: string` |

Extras da resposta stateless: `cacheScope: "public"`, `ttlMs: 300000`.
Resposta bruta em [`logs-stateless/tools-list-response.json`](logs-stateless/tools-list-response.json).

## Observação sobre "versão de cada ferramenta"

Igual ao CENÁRIO-002: o MCP não tem campo de versão **por ferramenta** — só `name`,
`description`, `inputSchema`. No modo stateless a versão do servidor chega em
`result._meta.io.modelcontextprotocol/serverInfo.version` = `v1.1.0-alpha-6-g439ea52`
(no modo com handshake, viria no `initialize`).

## Evidências

- [x] Log do servidor MCP com o endpoint no ar e a contagem de tools — [`logs-stateless/mcp-server.log`](logs-stateless/mcp-server.log)
- [x] Headers da resposta stateless (sem `Mcp-Session-Id`) — [`logs-stateless/tools-list-headers.txt`](logs-stateless/tools-list-headers.txt)
- [x] Resposta do `tools/list` stateless (lista completa + decorações) — [`logs-stateless/tools-list-response.json`](logs-stateless/tools-list-response.json)
- [x] Controle negativo sem `_meta` (`-32602`) — [`logs-stateless/tools-list-no-meta-response.json`](logs-stateless/tools-list-no-meta-response.json)
- [x] Controle negativo com versão inválida (`-32022`) — [`logs-stateless/tools-list-bad-version-response.json`](logs-stateless/tools-list-bad-version-response.json)
- [x] Trilha completa da execução do cliente — [`logs-stateless/client.log`](logs-stateless/client.log)

## Conclusão

**PASS.** No modo stateless (`2026-07-28`, `params._meta` sem `initialize` nem
`Mcp-Session-Id`), o `tools/list` devolve a lista completa: **2** ferramentas
(`DocPipeline`, `SlowBuild`) — mesma contagem do startup —, cada uma com `name`,
`description` e `inputSchema`, mais as decorações do modo stateless (`resultType: "complete"`,
`_meta.serverInfo`). O servidor não emite sessão e faz cumprir o contrato: `-32602` se faltar
`params._meta`, `-32022` para uma `protocolVersion` não suportada.
