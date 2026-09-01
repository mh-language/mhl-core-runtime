# ITEM-02 (alvo): sessões MCP compartilhadas entre réplicas

> **Escopo: distribuição + isolamento.** Uma sessão criada numa réplica deve
> valer em qualquer outra (distribuição), e a partição dos dados por principal
> — a identidade que o mesh já verificou via `--principal-header` — deve
> continuar valendo entre pods (isolamento). Nada aqui decide *quem pode*: só
> *de quem é* cada dado.

**Item do design:** 02 — *MCP sessions live in one pod's memory* (parcial).
**Estado hoje:** `h.sessions` é um `map` por pod; um `Mcp-Session-Id` cunhado no
Pod A é `404` no Pod B. `session/*` já é gravado no `extension store`, mas o
`h.sessions` em memória não é consultado a partir dele.

## Comportamento-alvo

Com duas réplicas compartilhando estado:

```gherkin
Dado initialize no Pod A -> Mcp-Session-Id sidA
Quando uma chamada (tools/list) chega ao Pod B com Mcp-Session-Id: sidA
Então o Pod B reconhece a sessão e responde 200 (sem forçar re-initialize)

Dado chamadas intercaladas na mesma sessão indo ora ao Pod A ora ao Pod B
Então todas funcionam — a sessão é a mesma dos dois lados
```

Sub-alvo (já funciona, deve continuar): com `--principal-header`, `run/list` /
`run/status` de `bob` no Pod B **não** enxergam a run de `alice` iniciada no
Pod A (`-32602`); `alice` enxerga.

## Critério de aceite

1. `tools/list` no Pod B com um `Mcp-Session-Id` cunhado no Pod A → HTTP `200`
   (sessão encontrada), **não** `404`.
2. `run/start` no Pod A e `run/status` no Pod B com **a mesma sessão** →
   funciona (não `-32602` por sessão desconhecida).
3. (regressão) `--principal-header`: isolamento por identidade continua valendo
   entre os dois pods.

## Como implementar (pistas)

- `internal/mcpserver/http.go`: `h.sessions` atrás do mesmo seam
  `SessionStore` que o `extension store` já preenche — `getSession` consulta o
  store no miss local antes de devolver 404.
- Alternativa: contrato de roteamento (chave estável do cliente + hash-route no
  ingress) — o ALB não consegue keyar num campo do corpo JSON, então o store é
  o caminho.

## Evidências (logs/)
- `A-sid.txt`, `B-foreign-session.txt` (deve ser 200)
- `B-crosspod-status.json`
- `C-bob-status.json`, `C-alice-status.json` (regressão do isolamento)

**Verificado por:** `./run.sh` — **MET**.

## Como foi implementado

`internal/mcpserver/store.go` ganhou `diskSessionStore`: um arquivo JSON por
`Mcp-Session-Id` em `<state-dir>/.mhl/state/sessions/`, atrás do mesmo seam
`SessionStore`. `buildHTTP` (`http.go`) troca o `memSessionStore` por ele quando
`cps.Shared()` (há `--state-dir`) e não há `extension store` (esse já usava
`extSessionStore`). Sem `--state-dir`, nada muda. Cobertura:
`TestHTTPSharedSessionsAcrossReplicas`, `TestDiskSessionStoreSharesAcrossInstances`.
