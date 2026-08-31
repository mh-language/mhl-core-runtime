# CENÁRIO-005 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-005-auth-guards.md`](CENARIO-005-auth-guards.md) não foi alterado.

## Cenário 005: Autenticação por bearer e guarda de Origin

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 12:32 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-005/mhl` (cópia de `sample/cloud/mhl`, reconstruído do source atual) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor principal | `mhl serve mcp --http --addr 127.0.0.1:8722 --token <gerado> --state-dir <tmp> sample/cloud` |
| 2º servidor (controle) | `mhl serve mcp --http --addr 0.0.0.0:8723 --state-dir <tmp> sample/cloud` (**sem** `--token`) |
| Script | [`run.sh`](run.sh) |

## Matriz observada ([`logs/auth-matrix.txt`](logs/auth-matrix.txt))

| Requisição | Esperado | Obtido |
|---|---|---|
| `POST /mcp` sem `Authorization` | 401 | **401** |
| `POST /mcp` bearer errado | 401 | **401** |
| `POST /mcp` bearer correto | 200 | **200** |
| `GET /healthz` sem bearer | 200 | **200** |
| `GET /metrics` sem bearer | 200 | **200** |
| `POST /mcp` `Origin: http://evil.example` | 403 (`forbidden origin`) | **403** |
| `POST /mcp` `Origin: http://localhost` | 200 (loopback aceito) | **200** |
| 2º processo em `0.0.0.0` sem `--token` | aviso no stderr | **aviso emitido** |

Aviso capturado ([`logs/mcp-server-notoken.log`](logs/mcp-server-notoken.log)):

```
mhl serve mcp --http: warning: binding 0.0.0.0:8723 with no --token/MHL_SERVE_TOKEN — the endpoint is unauthenticated
```

## Verificações (todas OK)

- `POST /mcp` só passa com o bearer exato (`Authorization: Bearer <token>`); sem header ou com token diferente → `401`.
- `/healthz` e `/metrics` respondem `200` sem qualquer credencial (probes / scrape de dentro do pod).
- Guard de DNS-rebinding: `Origin` presente e **não-loopback** → `403 forbidden origin`; `Origin` loopback (`http://localhost`) → passa.
- Bind não-loopback (`0.0.0.0`) **sem** `--token` → o servidor sobe mas alerta que o endpoint está desprotegido.

## Evidências

- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — servidor principal no ar (`2 tool(s) ... http://127.0.0.1:8722/mcp`)
- [x] [`logs/mcp-server-notoken.log`](logs/mcp-server-notoken.log) — 2º processo + linha de aviso `endpoint is unauthenticated`
- [x] [`logs/auth-matrix.txt`](logs/auth-matrix.txt) — matriz requisição → HTTP
- [x] [`logs/client.log`](logs/client.log) — trilha completa da execução

## Observações

- Testa só o guard **estático** do pod (`staticToken` / `trustedHeader` + `originAllowed`), não JWT/JWKS (item 10 do design segue aberto).
- `originAllowed` só bloqueia `Origin` **presente** e não-loopback — um cliente sem navegador que não envia `Origin` passa direto (comportamento esperado do guard anti DNS-rebinding).
- Ambos os servidores foram encerrados ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** O endpoint de protocolo (`POST /mcp`) exige o bearer compartilhado; os endpoints
operacionais (`/healthz`, `/metrics`) ficam livres; `Origin` cross-site não-loopback é
rejeitado com `403`; e subir em bind não-local sem `--token` gera o aviso de endpoint
não autenticado.
