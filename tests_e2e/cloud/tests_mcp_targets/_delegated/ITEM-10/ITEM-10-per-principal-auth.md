# ITEM-10 (alvo): auth por principal — tokens/escopos ou JWT/JWKS

> **Fora do escopo do runtime — delegado ao mesh.** Validação de JWT, tokens
> por time, escopos e rotação são do **Istio** (`RequestAuthentication` + JWKS)
> ou do **API Gateway** (Cognito / Lambda authorizer): o ingress valida a
> credencial e injeta o `sub` no header `X-Mhl-Principal`. O runtime só precisa
> **consumir** essa identidade — o que já faz com `--principal-header` (item 03,
> atendido): a identidade verificada vira o `owner` das runs e isola
> `run/list` / `run/status` / `run/logs` / `run/resume` por principal
> (CENARIO-006). O `--token` continua sendo apenas o segredo compartilhado
> gateway↔mhl (anti-spoof do header), não uma senha de aplicação.

**Item do design:** 10 — *Auth is a single static token* (aberto).
**Estado hoje:** um `--token` único, comparado por igualdade; sem JWT/JWKS,
tokens por time, escopos, expiry ou hot-reload.

## Comportamento-alvo

Pelo menos **um** destes modos:

**A) `--token-file` (tokens → principal + escopos)**
```gherkin
Dado tokens.json = { "tokA": {"principal":"team-a","scopes":["run"]},
                     "tokRO":{"principal":"auditor","scopes":["read"]} }
E `mhl serve mcp --http --token-file tokens.json`
Quando o cliente usa tokA
Então autentica como principal "team-a" e pode run/start
Quando o cliente usa tokRO
Então autentica como "auditor" e run/status/run/logs funcionam, mas run/start -> forbidden
Quando tokens.json é reescrito sem tokA e recarregado (SIGHUP / auto)
Então tokA passa a dar 401 sem reiniciar o processo
```

**B) `--jwks-url` (valida JWT)**
```gherkin
Dado `mhl serve mcp --http --jwks-url http://.../jwks.json`
Quando o cliente manda um JWT assinado pela chave do JWKS
Então autentica e o `sub` do JWT vira o principal
Quando o JWT está expirado ou mal assinado
Então 401
```

## Critério de aceite

1. O servidor aceita `--token-file` **ou** `--jwks-url` (ou ambos).
2. Dois principals com **credenciais distintas** (não o mesmo segredo).
3. Escopos: um principal `read` não consegue `run/start` (erro claro de
   autorização), mas consegue `run/status`.
4. Rotação sem restart: revogar uma credencial passa a valer.
   *(ou, no modo JWT: um JWT expirado → 401.)*

## Como implementar (pistas)

- `internal/mcpserver/verifier.go`: hoje `TokenVerifier` = token estático ou
  trusted-header. Adicionar `fileVerifier` (lê `--token-file`, `fsnotify`/SIGHUP
  para reload) e/ou `jwksVerifier` (busca JWKS, valida assinatura+`exp`,
  extrai `sub`/`scope`).
- `internal/cli/serve.go`: flags `--token-file`, `--jwks-url`.
- Escopo: `dispatch` checa `session.scopes` antes de `run/start` /
  `run/resume` (métodos de escrita) e devolve `-32001`/`-32603` com mensagem.

## Evidências (logs/)
- `flags.out` (o servidor aceitou `--token-file`/`--jwks-url`?)
- `teamA.txt`, `teamRO-start.json` (escopo), `rotated.txt`

**Verificado por:** `./run.sh` — hoje **PENDING**.
